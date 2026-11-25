"""Task-local build/validation entry point. Run from repo root through uv."""
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import tempfile
import time

ROOT = Path.cwd()
if not os.environ.get('BOOTSTRAP_EVIDENCE'):
    raise RuntimeError('set BOOTSTRAP_EVIDENCE to your invocation-owned .state directory')
STATE = Path(os.environ['BOOTSTRAP_EVIDENCE']).resolve()
STATE.mkdir(parents=True, exist_ok=True)
CRATE = ROOT / 'rust/bootstrap-rs'
ENV = dict(os.environ, CARGO_NET_OFFLINE='true', GOPROXY='off', GOTOOLCHAIN='local',
           BOOTSTRAP_GO_MAIN=str(STATE / 'go-main'),
           BOOTSTRAP_GO_PROBE=str(STATE / 'go-probe'),
           BOOTSTRAP_FIXTURE_DRIVER=str(STATE / 'fixture-driver'),
           BOOTSTRAP_RUST_PROBE=str(CRATE / 'target/release/bootstrap-probe'))
# The installed Go launcher may otherwise try to verify/download the cached toolchain.
# Select an already provisioned compiler; never fetch one in this offline runner.
if os.environ.get('BOOTSTRAP_GO_BIN'):
    ENV['PATH'] = os.environ['BOOTSTRAP_GO_BIN'] + os.pathsep + ENV['PATH']

def run(args, name, env=None, timeout=180):
    start = time.monotonic()
    with (STATE / (name + '.log')).open('wb') as log:
        proc = subprocess.Popen(args, env=env or ENV, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        code = 124
        try:
            code = proc.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            log.write(b'\nvalidation deadline exceeded\n')
        finally:
            # Every invocation owns its session; terminate descendants before returning.
            try:
                os.killpg(proc.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                proc.wait(timeout=2)
            except subprocess.TimeoutExpired:
                os.killpg(proc.pid, signal.SIGKILL)
                proc.wait(timeout=2)
    result = {'command': args, 'name': name, 'exit': code, 'seconds': round(time.monotonic()-start, 3)}
    with (STATE / 'commands.jsonl').open('a') as out:
        out.write(json.dumps(result)+'\n')
    print(json.dumps(result), flush=True)
    if code:
        print((STATE / (name+'.log')).read_text(errors='replace')[-7000:])
    return code

def prepare():
    with tempfile.TemporaryDirectory(prefix='bootstrap-build-',dir=ROOT/'.tmp') as scratch:
        scratch = Path(scratch)
        original = (ROOT/'scripts/dev-bootstrap.go').read_text()
        if original.count('func main()') != 1:
            raise RuntimeError('source main signature changed')
        (scratch/'original.go').write_text(original.replace('func main()', 'func originalMain()'))
        (scratch/'probe.go').write_bytes((ROOT/'rust/reference/bootstrap/probe.go.txt').read_bytes())
        env = dict(ENV,GOTMPDIR=str(scratch))
        for args,name in [(['go','build','-o',str(STATE/'go-main'),'scripts/dev-bootstrap.go'],'go-main-build'),
                          (['go','build','-o',str(STATE/'go-probe'),str(scratch/'original.go'),str(scratch/'probe.go')],'go-probe-build'),
                          (['go','build','-o',str(STATE/'fixture-driver'),'./rust/reference/bootstrap/driver'],'fixture-build')]:
            if run(args,name,env): return 1
    return 0

def inputs():
    cases = []
    def add(**kw): cases.append({'fn':'Scenario','args':[json.dumps(kw,ensure_ascii=True,separators=(',',':'))]})
    for op in ['command','bootstrap','run','main']:
        add(op=op,out=b'999.0.0-dev.test\n'.hex())
        for mode in ['missing-source','empty','denied','directory','format','interpreter','signal','missing-cli','symlink','existing-override']:
            add(op=op,mode=mode,out=b'999.0.0-dev.test\n'.hex(),err=b'  refused \r\n'.hex())
        for status in [1,2,127,255]:
            add(op=op,status=status,out=b'ignored'.hex(),err=b' \tbad\x00\xff\n'.hex())
        for out in [b'',b' \t\r\n',b'\xc2\xa0 version \xe3\x80\x80',b'v\x00\xff\n',b'a\nb\n',b"v'\";$()\\%\n"]:
            add(op=op,out=out.hex())
        for source in ['bin/a b','bin/quote\'\";$()%','bin/unicode-\u96ea','bin/line\nfeed']:
            add(op=op,source=source,out=b'v'.hex())
    for op in ['bootstrap','run','main']:
        for kw in [{'help':'old CLI'}, {'help_status':3}, {'help':'--bin','help_status':1},
                   {'register_status':4}, {'which_status':5}, {'help':'x--binx'},
                   {'destination':' \tcustom\npath \r\n'}, {'no_release':True},
                   {'unset':True}, {'dotfiles':'missing'}, {'dotfiles':'file'},
                   {'mode':'missing-source','help':'old'},
                   {'mode':'empty','help_status':1},
                   {'register_status':4,'which_status':5}]:
            add(op=op,out=b'v'.hex(),err=b'failure'.hex(),**kw)
    for op in ['bootstrap','command']:
        for absolute_source in [False,True]:
            for absolute_dotfiles in [False,True]:
                add(op=op,out=b'v'.hex(),remove_cwd=True,absolute_source=absolute_source,absolute_dotfiles=absolute_dotfiles)
    for mode in ['bad-cwd','lookup','relative-path','relative-executable','nul']:
        add(op='command',mode=mode,args=['tool','dev','use','--help'],out=b'v'.hex())
    add(op='main',args=['--help','ignored'],unset=True)
    add(op='command',args=['a b','line\narg','\x00'],out=b'v'.hex())
    # Exercise actual adapter errors through the original recorder, as distinct
    # from helper failures represented by successful scenario observations.
    for raw in ['{', '[]', '{"op":"unknown"}',
                '{"op":"command","source":"/outside"}',
                '{"op":"command","source":"../outside"}',
                '{"op":"command","dotfiles":"unknown"}',
                '{"op":"command","out":"g"}',
                '{"op":"command","err":"g"}',
                '{"op":"command","status":-1}',
                '{"op":"command","help_status":256}',
                '{"op":"command","register_status":-1}',
                '{"op":"command","which_status":256}']:
        cases.append({'fn': 'Scenario', 'args': [raw]})
    path=ROOT/'rust/reference/bootstrap-inputs.jsonl'
    path.write_text(''.join(json.dumps(x)+'\n' for x in cases))
    print(f'{len(cases)} deliberate inputs')

def coverage():
    with tempfile.TemporaryDirectory(prefix='bootstrap-coverage-', dir=ROOT/'.tmp') as scratch:
        env = dict(ENV, CARGO_LLVM_COV_TARGET_DIR=str(Path(scratch)/'target'))
        report = STATE/'coverage-final.json'
        code = run(['cargo','llvm-cov','--manifest-path',str(CRATE/'Cargo.toml'),'--offline','--locked',
                    '--json','--output-path',str(report)], 'coverage-final', env, timeout=600)
        if code: return code
        files = json.loads(report.read_text())['data'][0]['files']
        generated = {'lib.rs','wire.rs','entry.rs'}
        expected = {str(p.resolve()) for p in (CRATE/'src').glob('*.rs') if p.name not in generated}
        actual = {f['filename']: f['summary'] for f in files if f['filename'] in expected}
        if set(actual) != expected: raise RuntimeError('coverage omitted a first-party source')
        summary = {'files': actual, 'excluded': sorted(generated), 'groups': {}}
        for name, sources in [('first_party', expected), ('native', {str(CRATE/'src'/p) for p in ['bootstrap.rs','quote.rs','main.rs']})]:
            totals = {}
            for metric in ['lines','functions']:
                count = sum(actual[p][metric]['count'] for p in sources)
                covered = sum(actual[p][metric]['covered'] for p in sources)
                percent = covered * 100 / count
                totals[metric] = dict(covered=covered,count=count,percent=percent)
            summary['groups'][name] = totals
        (STATE/'coverage-summary.json').write_text(json.dumps(summary,indent=2)+'\n')
        print(json.dumps(summary['groups'],indent=2))
        if any(m['percent'] < 90 for g in summary['groups'].values() for m in g.values()):
            return 1
        # The exported counters have been consumed and verified before owned scratch removal.
    return 0

if __name__ == '__main__':
    action = sys.argv[1]
    if action == 'prepare': sys.exit(prepare())
    if action == 'inputs': inputs()
    if action == 'coverage': sys.exit(coverage())
    if action == 'run':
        # Mixed build/test commands retain system temporary-directory defaults.
        # prepare() scopes GOTMPDIR only to commands that compile without testing.
        sys.exit(run(sys.argv[3:],sys.argv[2],env=ENV,timeout=600))

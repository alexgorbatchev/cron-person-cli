"""Native Go/Rust interoperability, using owned helpers, deadlines and cleanup."""
import contextlib
import itertools
import os
from pathlib import Path
import queue
import signal
import subprocess
import sys
import tempfile
import threading
import time

TIMEOUT = 5
executables = dict(zip(('go', 'rust'), sys.argv[1:]))
checks = 0

class Process:
    def __init__(self, language, mode, path):
        self.p = subprocess.Popen([executables[language], mode, str(path)], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, start_new_session=os.name != 'nt')
        self.lines = queue.Queue()
        def read():
            for line in self.p.stdout:
                self.lines.put(line.strip())
        self.reader = threading.Thread(target=read)
        self.reader.start()
    def expect(self, text):
        actual = self.lines.get(timeout=TIMEOUT)
        assert actual == text, (text, actual)
    def send(self, text):
        self.p.stdin.write(text+'\n')
        self.p.stdin.flush()
    def finish(self, abrupt=False):
        if abrupt:
            self.p.kill()
        else:
            self.send('exit')
        self.p.wait(timeout=TIMEOUT)
    def close(self):
        if self.p.poll() is None:
            self.p.stdin.close()
            try:
                self.p.wait(timeout=1)
            except subprocess.TimeoutExpired:
                if os.name != 'nt': os.killpg(self.p.pid, signal.SIGKILL)
                else: self.p.kill()
                self.p.wait(timeout=TIMEOUT)
        # A helper that failed after spawning must not leave a descendant holding
        # our pipes or descriptors merely because the group leader already exited.
        if os.name != 'nt':
            try: os.killpg(self.p.pid, signal.SIGTERM)
            except ProcessLookupError: pass
        self.reader.join(timeout=TIMEOUT)
        assert not self.reader.is_alive(), 'helper pipe reader survived cleanup'
        for pipe in (self.p.stdin, self.p.stdout, self.p.stderr):
            pipe.close()

def probe(language, path, expected):
    global checks
    result = subprocess.run([executables[language], 'probe', str(path)], text=True, capture_output=True, timeout=TIMEOUT)
    assert result.returncode == 0, result.stderr
    assert result.stdout.strip() == expected, result.stdout
    checks += 1

start = time.monotonic()
with tempfile.TemporaryDirectory(prefix='filelock-native-') as fixture:
    root = Path(fixture)
    for holder_lang, waiter_lang in itertools.product(executables, repeat=2):
        actions = ['unlock', 'close', 'exit', 'kill', 'inherit']
        if os.name != 'nt': actions += ['inherited-close', 'inherited-unlock']
        for action in actions:
            path = root / f'{holder_lang}-{waiter_lang}-{action}'
            contents = b'ab' if os.name == 'nt' else b''
            if os.name == 'nt': path.write_bytes(contents)
            with contextlib.ExitStack() as owned:
                holder = Process(holder_lang, 'hold', path)
                owned.callback(holder.close)
                holder.expect('held')
                before = path.stat()
                if os.name != 'nt': assert before.st_mode & 0o777 == 0o600
                probe(waiter_lang, path, 'file is locked')
                if os.name == 'nt':
                    # The reference protects exactly byte 0, not std's much larger range.
                    with path.open('rb', buffering=0) as reader:
                        reader.seek(1)
                        assert reader.read(1) == b'b'
                        reader.seek(0)
                        try: reader.read(1)
                        except OSError as error: assert error.winerror == 33
                        else: raise AssertionError('locked first byte was readable')
                    for mutation in (lambda: path.rename(path.with_suffix('.moved')), path.unlink):
                        try: mutation()
                        except PermissionError as error: assert error.winerror == 32
                        else: raise AssertionError('live lock path could be renamed/deleted')
                    checks += 3
                if action in ('inherit', 'inherited-close', 'inherited-unlock'):
                    holder.send('spawn' if action == 'inherit' else 'spawn-inherit'); holder.expect('spawned')
                    holder.send('unlock' if action == 'inherited-unlock' else 'close'); holder.expect('ok')
                    probe(waiter_lang, path, 'file is locked' if action == 'inherited-close' else 'ok')
                    holder.finish()
                    probe(waiter_lang, path, 'ok')
                else:
                    waiter = Process(waiter_lang, 'block', path)
                    owned.callback(waiter.close)
                    waiter.expect('waiting')
                    # A bounded negative observation is necessary to prove blocking.
                    try:
                        early = waiter.lines.get(timeout=0.08)
                    except queue.Empty:
                        pass
                    else:
                        raise AssertionError(('blocking call returned while held', early))
                    if action in ('unlock', 'close'):
                        holder.send(action); holder.expect('ok')
                    else:
                        holder.finish(abrupt=action == 'kill')
                    waiter.expect('acquired')
                    probe(holder_lang, path, 'file is locked')
                    waiter.finish()
                    probe(holder_lang, path, 'ok')
                after = path.stat()
                assert before.st_ino == after.st_ino, 'lock file was replaced'
                assert path.read_bytes() == contents, 'lock file was written/truncated unexpectedly'
                checks += 1
    for language in executables:
        probe(language, root / 'missing' / 'lock', 'os:3' if os.name == 'nt' else 'os:2')
        probe(language, root, 'os:5' if os.name == 'nt' else 'os:21')
print(f'{checks} native assertions passed in {time.monotonic()-start:.3f}s; all four Go/Rust holder/waiter combinations')

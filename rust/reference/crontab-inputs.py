"""Deterministic, byte-preserving cases for production crontab APIs."""
import itertools
import json
import pathlib
import random

start = b'# >>> cron-person managed block (DO NOT EDIT DIRECTLY) >>>'
end = b'# <<< cron-person managed block <<<'
cases = []
def text(op, existing, managed=b''):
    cases.append(dict(fn='Text', args=[op, existing.hex(), managed.hex()]))

# All short marker permutations include competing errors in encounter order.
for n in range(5):
    for lines in itertools.product([start, end, b'job', b''], repeat=n):
        for separator in [b'\n', b'\r\n']:
            data = separator.join(lines)
            text('remove', data)
            text('merge', data, start+b'\nnew\n'+end)
for existing in [b'', b'\n', b'\n\n', b'job\r\n\n', bytes(range(256)), b'\xff\0\r\n',
                 b'job '+start+b'\n'+end+b' suffix\n', b' '+start+b'\n', start+b'\r',
                 start+b'\r\nbody\r\n'+end+b'\r', start+b'\nbody\n'+end+b'\n\nsuffix']:
    for block in [b'', b'\n', b'new', b'new\n', bytes(range(256))]:
        text('merge', existing, block)
randomizer = random.Random(148732)
for _ in range(80):
    data = randomizer.randbytes(randomizer.randrange(500))
    text('remove', data)
    text('merge', data, randomizer.randbytes(100))
for args in [('remove', 'x', 'y'), ('merge', '', 'z'), ('other', '', ''), ('remove', '0', '')]:
    cases.append(dict(fn='Text', args=args))

def process(**fields):
    request = dict(operation='read', mode='normal', stdout='', stderr='', content='', status=0)
    request.update(fields)
    cases.append(dict(fn='Process', args=[json.dumps(request, separators=(',', ':'))]))

for op in ['read', 'write']:
    for mode in ['normal','missing','denied','format','interpreter','relative','closed','signal']:
        process(operation=op, mode=mode, stdout=b'output\0\xff\r\n'.hex(), stderr=b'error\0\xff\n'.hex(), content=bytes(range(256)).hex(), status=7 if mode == 'normal' else 0)
    for status in [0, 1, 2, 127, 255]:
        for error in [b'', b'warning\n', b'no crontab for test\n', b'prefix no crontab for\0suffix\xff']:
            process(operation=op, status=status, stdout=b'job\r\n\xff\0\n'.hex(), stderr=error.hex(), content=b'entry\0\xff\r\n'.hex())
    process(operation=op, stdout=(b'o'*131072).hex(), stderr=(b'e'*131072).hex(), content=(b'c'*131072).hex())
    process(operation=op, mode='closed', content=(b'x'*1048576).hex())
for change in [dict(operation='other'),dict(status=-1),dict(status=256),dict(stdout='z'),dict(stderr='z'),dict(content='z'),dict(mode='other')]:
    process(**change)
cases.append(dict(fn='Process', args=['invalid']))
pathlib.Path('rust/reference/crontab-inputs.jsonl').write_text(''.join(json.dumps(c, ensure_ascii=False)+'\n' for c in cases))
print(f'{len(cases)} deterministic cases')

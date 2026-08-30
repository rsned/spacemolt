#!/usr/bin/env python3
"""Audit play_as formatter structs against server_docs/openapi.json.

The failure class this hunts (first seen 2026-08-30): a formatter-local
struct field whose Go type conflicts with the type the server now sends, so
json.Unmarshal errors and the styled formatter silently falls back to raw
JSON. wreckEntry.Modules ([]string vs the v0.572 module objects) hid the
styled wrecks view for a day; coverage_pct (string vs spec number) was the
other live hit. Run after every update-server-docs refresh.

Output: HARD CONFLICTS (the local Go kind matches no spec occurrence of the
tag — investigate every one; play_as-authored plan/serialization structs
like sellable/unload are known false positives) and AMBIGUOUS array tags
(the spec uses several array kinds for the name across schemas — verify the
formatter's shape against the specific command it parses).
"""
import json, re, glob

schemas = json.load(open('server_docs/openapi.json'))['components']['schemas']
spec = {}

def kind(v):
    if not isinstance(v, dict):
        return 'any'
    if '$ref' in v:
        return 'object'
    t = v.get('type')
    if t == 'array':
        it = v.get('items', {})
        if not isinstance(it, dict):
            return 'array-any'
        if '$ref' in it or it.get('type') == 'object':
            return 'array-object'
        return 'array-' + (it.get('type') or 'any')
    return t or ('object' if 'properties' in v else 'any')

for sc in schemas.values():
    for n, v in (sc.get('properties', {}) or {}).items():
        spec.setdefault(n, set()).add(kind(v))

FIELD = re.compile(r'^\s*[A-Z]\w*\s+(\[\])?(\*)?([\w.]+)\s+`json:"([a-z0-9_]+)')
BASE = {'string': 'string', 'bool': 'boolean', 'int': 'number', 'int64': 'number',
        'int32': 'number', 'float64': 'number', 'float32': 'number'}

def gokind(arr, typ):
    t = typ.split('.')[-1]
    if arr:
        if t == 'string':
            return 'array-string'
        if t in BASE:
            return 'array-' + ('number' if BASE[t] == 'number' else BASE[t])
        return 'array-object'
    return BASE.get(t)

def compat(gk, sk):
    if gk is None or sk in ('any', 'array-any'):
        return True
    if gk == sk:
        return True
    if gk == 'number' and sk in ('integer', 'number'):
        return True
    if gk == 'array-number' and sk in ('array-integer', 'array-number'):
        return True
    return False

hard, amb = [], []
for path in sorted(glob.glob('cmd/tools/play_as/*.go')):
    if path.endswith('_test.go'):
        continue
    for ln, line in enumerate(open(path), 1):
        m = FIELD.match(line)
        if not m:
            continue
        arr, _, typ, tag = m.groups()
        if typ in ('json.RawMessage', 'any') or typ.startswith('map'):
            continue
        gk = gokind(bool(arr), typ)
        ks = spec.get(tag)
        if not ks or gk is None:
            continue
        row = (path.split('/')[-1], ln, tag, gk, ','.join(sorted(ks)))
        if not any(compat(gk, k) for k in ks):
            hard.append(row)
        elif gk.startswith('array') and len([k for k in ks if k.startswith('array')]) > 1:
            amb.append(row)

print("HARD CONFLICTS:")
for h in hard:
    print("  %s:%d  %-22s go:%-13s spec:%s" % h)
print(f"\nAMBIGUOUS ({len(amb)}):")
for a in amb:
    print("  %s:%d  %-22s go:%-13s spec:%s" % a)

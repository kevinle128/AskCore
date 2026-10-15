import re,sys,unicodedata
def cw(s): return sum(0 if unicodedata.combining(c) else (2 if unicodedata.east_asian_width(c) in 'WF' else 1) for c in s)
t=open(sys.argv[1]).read()
cur=None;n=bad=0
for m in re.finditer(r'(\d+)×(\d+):\n|```text\n(.*?)```', t, re.S):
    if m.group(1): cur=int(m.group(1)); continue
    if cur is None: continue
    for l in m.group(3).splitlines():
        n+=1
        if cw(l)!=cur: bad+=1; print('BAD',cur,cw(l),repr(l))
print('checked lines',n,'bad',bad)

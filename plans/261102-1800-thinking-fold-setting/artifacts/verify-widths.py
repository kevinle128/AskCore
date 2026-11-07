import re,sys,unicodedata
# Checks that every sketch line has the stated width and every sketch has the stated height.
def cw(s): return sum(0 if unicodedata.combining(c) else (2 if unicodedata.east_asian_width(c) in 'WF' else 1) for c in s)
t=open(sys.argv[1]).read()
cur=None;hgt=None;n=bad=0;sk=0
for m in re.finditer(r'(\d+)×(\d+):\n|```text\n(.*?)```', t, re.S):
    if m.group(1): cur=int(m.group(1)); hgt=int(m.group(2)); continue
    if cur is None: continue
    lines=m.group(3).splitlines(); sk+=1
    if len(lines)!=hgt: bad+=1; print('BAD HEIGHT',cur,hgt,len(lines))
    for l in lines:
        n+=1
        if cw(l)!=cur: bad+=1; print('BAD',cur,cw(l),repr(l))
print('checked sketches',sk,'lines',n,'bad',bad)
sys.exit(1 if bad else 0)

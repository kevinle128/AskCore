import sys,os,json,uuid,time,select
from pathlib import Path
sys.path.insert(0,str(Path.cwd()/'e2e/tui'))
from run import Session,assert_transcript,assert_no_replay
p=Path(__file__).parent
results=[]
for backend in ['alacritty','ghostty']:
 d=p/'resize-evidence'/'native-pending-checked'/backend;d.mkdir(parents=True,exist_ok=True)
 paths=[d/'control.fifo',d/'status.fifo']
 for path in paths:os.mkfifo(path)
 control=os.open(paths[0],os.O_RDWR|os.O_NONBLOCK);status=os.open(paths[1],os.O_RDWR|os.O_NONBLOCK)
 wrapper=d/'launch.py'
 wrapper.write_text('#!/usr/bin/env python3\nimport os,sys\nc=os.open('+repr(str(paths[0]))+',os.O_RDWR)\ns=os.open('+repr(str(paths[1]))+',os.O_RDWR)\nos.dup2(c,3,inheritable=True)\nos.dup2(s,4,inheritable=True)\nos.set_inheritable(3,True)\nos.set_inheritable(4,True)\nenv=dict(os.environ,T0_CONTROL="1",T0_TERMINAL_REFLOW="native")\nos.execve('+repr(str(p/'candidate'))+',['+repr(str(p/'candidate'))+']+sys.argv[1:],env)\n');wrapper.chmod(0o755)
 lines=[];buffer=b''
 def wait_status(wanted):
  global buffer
  deadline=time.monotonic()+10
  while time.monotonic()<deadline:
   if any(wanted(line) for line in lines):return
   ready,_,_=select.select([status],[],[],max(0,deadline-time.monotonic()))
   if ready:
    buffer+=os.read(status,65536)
    while b'\n' in buffer:
     line,buffer=buffer.split(b'\n',1);lines.append(line.decode())
  raise AssertionError('status predicate failed: '+repr(lines))
 s=None;record={'backend':backend,'result':'fail'}
 try:
  s=Session('tui-test','askcore-pending-'+uuid.uuid4().hex[:8],backend,wrapper,p,d/'session','g1')
  s.write('draft');s.cursor(13,'editor> draft')
  os.write(control,b'g1-pending-resize\n');wait_status(lambda line:line=='pending-write')
  s.resize(52,15)
  os.write(control,b'release-write\n');wait_status(lambda line:line=='size 52 15');wait_status(lambda line:line=='commit 2000 2000 false')
  s.wait(lambda state:'tail 2000' in state['text']);assert_transcript(s.text());assert_no_replay(s.recording())
  s.cursor(13,'editor> draft');s.capture('pending-resize')
  s.key('ctrl+c');wait_status(lambda line:line=='restored');os.write(control,b'exit\n');s.call('wait','exit','--timeout','5000')
  record['result']='pass'
 except Exception as error:
  record['error']=str(error)
  if s:s.capture('failure')
 finally:
  if s:s.close()
  for fd in [control,status]:os.close(fd)
  for path in paths:path.unlink()
  (d/'status-lines.json').write_text(json.dumps(lines,indent=2))
 results.append(record);print(record,flush=True)
(p/'resize-evidence'/'native-pending-checked-results.json').write_text(json.dumps(results,indent=2))
sys.exit(0 if all(r['result']=='pass' for r in results) else 1)

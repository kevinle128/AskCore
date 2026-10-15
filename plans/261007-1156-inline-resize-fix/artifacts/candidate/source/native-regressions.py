import sys,json,uuid
from pathlib import Path
sys.path.insert(0,str(Path.cwd()/'e2e/tui'))
from run import Session,assert_editor
p=Path(__file__).parent
cases=[('middle','abc界😀Z',3),('hard-lines','abc界😀Z\r\nsecond界😀Z',2),('long','abcdefghijklmnopqrstuvwxyz012345界😀Z',0),('trailing-space','abcdefghijklmnopqrstuvwxyz01234 界😀Z',0)]
results=[]
for backend in ['alacritty','ghostty']:
 for case,text,left in cases:
  s=None
  result={'backend':backend,'case':case,'result':'fail'}
  try:
   s=Session('tui-test','askcore-native-'+uuid.uuid4().hex[:8],backend,p/'candidate',p,p/'resize-evidence'/'strengthened-ready'/backend/case,'g6')
   s.write('\x1b[200~'+text+'\x1b[201~')
   if left:s.key(*(['left']*left))
   if case=='middle':s.cursor(11,'editor> abc界😀Z')
   elif case=='hard-lines':s.cursor(8,'second界😀Z')
   else:s.cursor(5,'界😀Z')
   s.capture('initial')
   for w,h in [(13,6),(12,6),(40,12),(13,6),(40,4),(40,12)]:
    prior=len(s.recording())
    s.resize(w,h)
    s.wait(lambda state: len(s.recording())>prior)
    if case=='middle':
     first='editor> abc界' if w==13 else 'editor> abc' if w==12 else 'editor> abc界😀Z'
     st=s.cursor(11,first)
     assert_editor(st,first)
    else:
     st=s.wait(lambda st: ('Z' in st['text']) and st['cursor']['x']<w)
     # Capture every dimension for independent hand-written matrix review.
     (s.artifacts/f'{w}-{h}-state.json').write_text(json.dumps(st,ensure_ascii=False))
     if w==40:
      expected={'hard-lines':'second界😀Z','long':'界😀Z','trailing-space':'界😀Z'}[case]
      x=8 if case=='hard-lines' else 5
      s.cursor(x,expected)
      assert s.state()['text'].count('editor>')==1,s.state()
    s.capture(f'{w}-{h}')
   report=s.finish()
   assert report['Text']==text.replace('\r\n','\n')
   result['result']='pass'
  except Exception as error:
   result['error']=str(error)
   if s:s.capture('failure')
  finally:
   if s:s.close()
  results.append(result);print(result,flush=True)
(p/'resize-evidence'/'strengthened-ready-results.json').write_text(json.dumps(results,ensure_ascii=False,indent=2))
sys.exit(0 if all(r['result']=='pass' for r in results) else 1)

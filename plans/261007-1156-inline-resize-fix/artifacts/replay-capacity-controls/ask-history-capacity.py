import os,sys,time,tty
tty.setraw(0)
if sys.argv[1]=="purge":os.write(1,b"\x1b[2J\x1b[3J\x1b[H")
os.write(1,("\r\n".join("G1[%04d]"%i for i in range(2000))+"\r\nDONE").encode())
time.sleep(120)

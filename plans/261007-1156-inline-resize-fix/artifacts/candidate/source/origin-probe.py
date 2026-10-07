#!/usr/bin/env python3
import os,signal,time
os.write(1,b'T0 startup\r\n\x1b7editor> abc'+ '界😀Z'.encode()+b'\r\x1b[11C')
def resize(*args):
    os.write(1,b'\x1b8\r\x1b[Jeditor> abc'+ '界😀Z'.encode()+b'\r\x1b[3C')
signal.signal(signal.SIGWINCH,resize)
while True:signal.pause()

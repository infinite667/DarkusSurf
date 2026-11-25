"""Generate a Windows COFF icon resource from assets/darkus.ico (requires clang)."""
import pathlib, struct, subprocess, tempfile
root=pathlib.Path(__file__).resolve().parents[1]; ico=(root/'assets/darkus.ico').read_bytes(); reserved,kind,count=struct.unpack_from('<HHH',ico)
assert reserved==0 and kind==1 and count>0
entries=[struct.unpack_from('<BBBBHHII',ico,6+16*i) for i in range(count)]
lines=['.section .rsrc,"dr"','.p2align 2','root:']
def directory(n):lines.extend(['.long 0,0','.short 0,0,0,'+str(n)])
def sub(id,label):lines.extend([f'.long {id}',f'.long ({label}-root)+0x80000000'])
def leaf(id,label):lines.extend([f'.long {id}',f'.long {label}-root'])
directory(2);sub(3,'icons');sub(14,'groups');lines+=['icons:'];directory(count)
for i in range(count):sub(i+1,f'lang{i}')
lines+=['groups:'];directory(1);sub(1,'group_lang')
for i in range(count):lines += [f'lang{i}:'];directory(1);leaf(1033,f'dataentry{i}')
lines+=['group_lang:'];directory(1);leaf(1033,'group_entry')
for i,e in enumerate(entries):lines += [f'dataentry{i}:',f'.rva image{i}',f'.long {e[6]},0,0']
group=struct.pack('<HHH',0,1,count)+b''.join(struct.pack('<BBBBHHIH',*e[:7],i+1) for i,e in enumerate(entries));lines+=['group_entry:','.rva group_data',f'.long {len(group)},0,0']
def data(label,b):
 lines.extend(['.p2align 2',label+':']);lines.extend('.byte '+','.join(str(x) for x in b[i:i+32]) for i in range(0,len(b),32))
for i,e in enumerate(entries):data(f'image{i}',ico[e[7]:e[7]+e[6]])
data('group_data',group)
with tempfile.TemporaryDirectory() as d:
 src=pathlib.Path(d)/'icon.s';src.write_text('\n'.join(lines)+'\n');subprocess.run(['clang','--target=x86_64-w64-windows-gnu','-c',str(src),'-o',str(root/'icon_windows_amd64.syso')],check=True)
# Go's internal PE resource linker uses the relocation addend, not symbol value.
# Normalize COFF relocations to the .rsrc section base and store their offsets.
p=root/'icon_windows_amd64.syso'; b=bytearray(p.read_bytes()); _,n,_,symoff,_,opt,_=struct.unpack_from('<HHIIIHH',b)
for i in range(n):
 s=20+opt+40*i
 if b[s:s+8].rstrip(b'\0')!=b'.rsrc':continue
 raw,rel=struct.unpack_from('<II',b,s+20); nr=struct.unpack_from('<H',b,s+32)[0]
 for j in range(nr):
  off=rel+10*j; addr,idx,typ=struct.unpack_from('<IIH',b,off); value=struct.unpack_from('<I',b,symoff+18*idx+8)[0]; old=struct.unpack_from('<I',b,raw+addr)[0];struct.pack_into('<I',b,raw+addr,old+value)
p.write_bytes(b)

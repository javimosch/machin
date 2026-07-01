package main
import ("fmt";"os";"strconv";"time")
func main(){
 n,_:=strconv.Atoi(os.Args[1])
 s:="the quick brown fox jumps over the lazy dog 0123456789"
 cnt:=0
 t0:=time.Now()
 for i:=0;i<n;i++{
  for j:=0;j<len(s)-5;j++{
   sub:=s[j:j+5]   // zero-copy in Go
   cnt+=len(sub)
  }
 }
 fmt.Printf("str cnt=%d ms=%d\n",cnt,time.Since(t0).Milliseconds())
}

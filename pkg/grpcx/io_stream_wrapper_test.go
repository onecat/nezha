package grpcx

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/naiba/nezha/proto"
)

type testIOStream struct {
	frames [][]byte
	end error
}
func (s *testIOStream) Context() context.Context { return context.Background() }
func (s *testIOStream) Send(*proto.IOStreamData) error { return nil }
func (s *testIOStream) Recv() (*proto.IOStreamData,error) {
	if len(s.frames)==0 {
		if s.end!=nil { return nil,s.end }
		return nil,io.EOF
	}
	data:=s.frames[0]
	s.frames=s.frames[1:]
	return &proto.IOStreamData{Data:data},nil
}

func TestLegacyNATEOF(t *testing.T) {
	for _,tc:=range []struct{
		name string
		frames []string
		nat bool
		want string
	}{
		{"legacy close",[]string{"HTTP body","EOF"},true,"HTTP body"},
		{"payload EOF then sentinel",[]string{"header","EOF","EOF"},true,"headerEOF"},
		{"nonfinal EOF",[]string{"EOF","data","EOF"},true,"EOFdata"},
		{"embedded EOF",[]string{"dataEOF","EOF"},true,"dataEOF"},
		{"ordinary stream",[]string{"data","EOF"},false,"dataEOF"},
		{"no sentinel",[]string{"data"},true,"data"},
	} {
		t.Run(tc.name,func(t *testing.T){
			s:=&testIOStream{}
			for _,frame:=range tc.frames {s.frames=append(s.frames,[]byte(frame))}
			var w *IOStreamWrapper
			if tc.nat {w=NewLegacyNATStreamWrapper(s)} else {w=NewIOStreamWrapper(s)}
			var got []byte
			buf:=make([]byte,2)
			for {
				n,err:=w.Read(buf)
				got=append(got,buf[:n]...)
				if err==io.EOF {break}
				if err!=nil {t.Fatal(err)}
			}
			if string(got)!=tc.want {t.Fatalf("got %q want %q",got,tc.want)}
		})
	}
}

func TestLegacyNATNonEOFErrorPreservesData(t *testing.T) {
	failure:=errors.New("transport failure")
	w:=NewLegacyNATStreamWrapper(&testIOStream{frames:[][]byte{[]byte("EOF")},end:failure})
	got,err:=io.ReadAll(w)
	if string(got)!="EOF" || !errors.Is(err,failure) {t.Fatalf("got %q error %v",got,err)}
}

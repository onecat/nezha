package agentupgrade_test

import (
 "os"
 "strings"
 "testing"
)

// The UI must never accidentally advertise an active upgrade while the
// runtime upgrade endpoint, authorization and rollback remain unfinished.
func TestDashboardUpgradeEntryFailClosed(t *testing.T) {
 data,err:=os.ReadFile("../../resource/template/dashboard-default/server.html")
 if err!=nil { t.Fatal(err) }
 s:=string(data)
 if !strings.Contains(s,"升级 Agent") || !strings.Contains(s,"disabled") ||
  !strings.Contains(s,"旧版 v0.20.5 需要手动迁移") {
  t.Fatal("missing disabled upgrade entry and legacy migration notice")
 }
 start:=strings.Index(s,"aria-label=\"升级 Agent（尚未开放）\"")
 if start<0 { t.Fatal("upgrade entry label missing") }
 prefix:=s[:start]
 pos:=strings.LastIndex(prefix,"<button")
 if pos<0 || !strings.Contains(prefix[pos:],"disabled") || !strings.Contains(prefix[pos:],"type=\"button\"") {
  t.Fatal("upgrade button must remain disabled")
 }
 end:=strings.Index(s[start:],"</button>")
 if end<0 || strings.Contains(s[pos:start+end],"onclick=") {
  t.Fatal("upgrade button must not dispatch requests")
 }
}

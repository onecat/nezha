package controller

import(
 "net"
 "strconv"
 "strings"
 "github.com/naiba/nezha/service/upgradecontrol"
)

func shellQuote(s string)string{return "'"+strings.ReplaceAll(s,"'","'\\''")+"'"}
func agentInstallCommand(selection upgradecontrol.ReleaseSelection,host string,port uint,secret string)string{
 digest:=selection.SHA256
 if !selection.Valid()||host==""||port==0||port>65535||secret==""{return ""}
 endpoint:=net.JoinHostPort(strings.Trim(host,"[]"),strconv.FormatUint(uint64(port),10))
 return "(umask 077; tmp=$(mktemp) || exit 1; trap 'rm -f \"$tmp\"' 0; curl -fsSL --proto '=https' --proto-redir '=https' --connect-timeout 15 --max-time 180 -o \"$tmp\" "+shellQuote(selection.URL)+" && printf '%s  %s\\n' "+shellQuote(digest)+" \"$tmp\" | sha256sum -c --status - && chmod 700 \"$tmp\" && printf '%s\\n' "+shellQuote(secret)+" | \"$tmp\" service install --server "+shellQuote(endpoint)+" --ca-file \"${NZ_AGENT_CA_FILE:-}\")"
}

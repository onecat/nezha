package controller

import(
 "encoding/hex"
 "net"
 "strconv"
 "strings"
)

// Set only by the release pipeline after verifying the exact independent
// Agent tag, binary and published checksum. Development builds fail closed.
var agentInstallSHA256 string
const agentInstallURL="https://github.com/onecat/nezha-agent/releases/download/v0.30.04/nezha-agent"

func agentInstallerDigest()string{
 b,err:=hex.DecodeString(agentInstallSHA256)
 if err!=nil||len(b)!=32||hex.EncodeToString(b)!=agentInstallSHA256{return ""}
 return agentInstallSHA256
}
func shellQuote(s string)string{return "'"+strings.ReplaceAll(s,"'","'\\''")+"'"}
func agentInstallCommand(host string,port uint,secret string)string{
 digest:=agentInstallerDigest()
 if digest==""||host==""||port==0||port>65535||secret==""{return ""}
 endpoint:=net.JoinHostPort(strings.Trim(host,"[]"),strconv.FormatUint(uint64(port),10))
 return "(umask 077; tmp=$(mktemp) || exit 1; trap 'rm -f \"$tmp\"' 0; curl -fsSL --proto '=https' --proto-redir '=https' --connect-timeout 15 --max-time 180 -o \"$tmp\" "+shellQuote(agentInstallURL)+" && printf '%s  %s\\n' "+shellQuote(digest)+" \"$tmp\" | sha256sum -c --status - && chmod 700 \"$tmp\" && printf '%s\\n' "+shellQuote(secret)+" | \"$tmp\" service install --server "+shellQuote(endpoint)+" --ca-file \"${NZ_AGENT_CA_FILE:-}\")"
}

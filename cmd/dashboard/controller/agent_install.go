package controller

import (
 _ "embed"
 "crypto/sha256"
 "fmt"
 "net"
 "net/http"
 "net/url"
 "strconv"
 "strings"

 "github.com/gin-gonic/gin"
)

//go:embed agent-install.sh
var agentInstaller []byte

func agentInstallerDigest()string{return fmt.Sprintf("%x",sha256.Sum256(agentInstaller))}

func serveAgentInstaller(c *gin.Context){
 digest:=agentInstallerDigest()
 if expected:=c.Query("sha256");expected!=""&&expected!=digest{c.AbortWithStatus(http.StatusConflict);return}
 c.Header("X-Agent-Installer-SHA256",digest)
 c.Header("Cache-Control","no-store")
 c.Header("X-Content-Type-Options","nosniff")
 c.Data(http.StatusOK,"text/plain; charset=utf-8",agentInstaller)
}

func shellQuote(s string)string{return "'"+strings.ReplaceAll(s,"'","'\\''")+"'"}
func installerOrigin(host string)(string,bool){
 if host==""||strings.ContainsAny(host,"/\\?#@% \t\r\n"){return "",false}
 u,err:=url.Parse("https://"+host)
 if err!=nil||u.Host!=host||u.User!=nil||u.Hostname()==""||u.Path!=""||u.RawQuery!=""||u.Fragment!=""{return "",false}
 if p:=u.Port();p!=""{n,e:=strconv.Atoi(p);if e!=nil||n<1||n>65535{return "",false}}
 return u.String(),true
}
func agentInstallCommand(origin,host string,port uint,secret string)string{
 if origin==""||host==""||port==0||port>65535||secret==""{return ""}
 endpoint:=net.JoinHostPort(strings.Trim(host,"[]"),strconv.FormatUint(uint64(port),10))
 digest:=agentInstallerDigest()
 return "(umask 077; tmp=$(mktemp) || exit 1; trap 'rm -f \"$tmp\"' 0; curl -fsSL --proto '=https' --proto-redir '=https' --connect-timeout 15 --max-time 60 -o \"$tmp\" "+shellQuote(origin+"/agent-install.sh?sha256="+digest)+" && printf '%s  %s\\n' "+shellQuote(digest)+" \"$tmp\" | sha256sum -c --status - && printf '%s\\n' "+shellQuote(secret)+" | bash \"$tmp\" "+shellQuote(endpoint)+")"
}

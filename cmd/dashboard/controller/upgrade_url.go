package controller

import(
 "context"
 "encoding/json"
 "io"
 "net/http"
 "strings"
 "time"
 "github.com/gin-gonic/gin"
)
func upgradeJSON(c *gin.Context,out any)bool{
 c.Request.Body=http.MaxBytesReader(c.Writer,c.Request.Body,8192)
 if c.ContentType()!="application/json"{c.AbortWithStatus(http.StatusUnsupportedMediaType);return false}
 dec:=json.NewDecoder(c.Request.Body);dec.DisallowUnknownFields()
 if err:=dec.Decode(out);err!=nil{c.AbortWithStatus(http.StatusBadRequest);return false}
 if err:=dec.Decode(new(any));err!=io.EOF{c.AbortWithStatus(http.StatusBadRequest);return false}
 return true
}
func(ma *memberAPI) agentInstallSetting(c *gin.Context){
 control,id,ok:=upgradeAccess(c);if !ok{return}
 if !control.Automatic(){c.AbortWithStatus(http.StatusServiceUnavailable);return}
 if c.Request.Method==http.MethodGet{
  selection,err:=control.Selection(c.Request.Context())
  if err!=nil{c.AbortWithStatus(http.StatusServiceUnavailable);return}
  c.Header("Cache-Control","no-store")
  c.JSON(http.StatusOK,selection);return
 }
 var in struct{URL string `json:"url"`}
 if !upgradeJSON(c,&in){return}
 ctx,cancel:=context.WithTimeout(c.Request.Context(),3*time.Minute);defer cancel()
 selection,err:=control.SaveSelection(ctx,id,strings.TrimSpace(in.URL))
 if err!=nil{c.JSON(http.StatusConflict,gin.H{"message":"验证失败，原设置保留："+err.Error()});return}
 c.JSON(http.StatusOK,selection)
}
func(ma *memberAPI) upgradePreview(c *gin.Context){
 control,id,ok:=upgradeAccess(c);if !ok{return}
 var in struct{Servers []uint64 `json:"servers"`}
 if !upgradeJSON(c,&in){return}
 p,err:=control.Preview(c.Request.Context(),id,in.Servers)
 if err!=nil{c.JSON(http.StatusConflict,gin.H{"message":"无法预览：请先验证并保存 Agent 安装设置，选择 1–3 台服务器"});return}
 c.Header("Cache-Control","no-store");c.JSON(http.StatusOK,p)
}
func(ma *memberAPI) upgradeURL(c *gin.Context){
 control,id,ok:=upgradeAccess(c);if !ok{return}
 if !control.Automatic(){c.AbortWithStatus(http.StatusServiceUnavailable);return}
 var in struct{Servers []uint64 `json:"servers"`;Revision uint64 `json:"revision"`}
 if !upgradeJSON(c,&in){return}
 ctx,cancel:=context.WithTimeout(c.Request.Context(),3*time.Minute);defer cancel()
 tasks,err:=control.UpgradeSelected(ctx,id,in.Servers,in.Revision)
 if err!=nil{c.JSON(http.StatusConflict,gin.H{"message":"升级未全部创建：设置、校验值或目标状态发生变化，请重新预览；不要重复提交已有任务","tasks":tasks});return}
 c.JSON(http.StatusOK,gin.H{"tasks":tasks})
}

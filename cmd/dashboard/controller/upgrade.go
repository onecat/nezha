package controller

import (
 "context"
 "encoding/json"
 "io"
 "net/http"
 "time"

 "github.com/gin-gonic/gin"
 "github.com/naiba/nezha/model"
 core "github.com/naiba/nezha/pkg/agentupgrade"
 "github.com/naiba/nezha/service/upgradecontrol"
)
func upgradeOperator(c *gin.Context)(uint64,bool){
 u,ok:=c.Get(model.CtxKeyAuthorizedUser)
 user,ok2:=u.(*model.User)
 if !ok||!ok2||user==nil||!user.SuperAdmin||c.GetBool("isAPI")||!localSecureCookie(c)||!upgradecontrol.Operator(c.Request.Context(),user.ID){return 0,false}
 return user.ID,true
}
func upgradeAccess(c *gin.Context)(*upgradecontrol.Control,uint64,bool){
 id,ok:=upgradeOperator(c)
 if !ok{c.AbortWithStatus(http.StatusForbidden);return nil,0,false}
 control:=upgradecontrol.Current
 if control==nil||!control.Execution{c.AbortWithStatus(http.StatusServiceUnavailable);return nil,0,false}
 return control,id,true
}
func(ma *memberAPI) uploadUpgrade(c *gin.Context){
 control,_,ok:=upgradeAccess(c)
 if !ok{return}
 if c.ContentType()!="application/octet-stream"{c.AbortWithStatus(http.StatusUnsupportedMediaType);return}
 // Require bounded upload reads even when the client stalls.
 rc:=http.NewResponseController(c.Writer)
 if err:=rc.SetReadDeadline(time.Now().Add(2*time.Minute));err!=nil{c.AbortWithStatus(http.StatusServiceUnavailable);return}
 defer rc.SetReadDeadline(time.Time{})
 c.Request.Body=http.MaxBytesReader(c.Writer,c.Request.Body,core.MaxPackageSize)
 ctx,cancel:=context.WithTimeout(c.Request.Context(),2*time.Minute)
 defer cancel()
 p,err:=control.Scheduler.Import(ctx,c.Request.Body)
 if err!=nil{c.JSON(http.StatusBadRequest,gin.H{"message":"升级包导入失败"});return}
 c.JSON(http.StatusOK,p)
}
func(ma *memberAPI) admitUpgradeWave(c *gin.Context){
 control,id,ok:=upgradeAccess(c)
 if !ok{return}
 c.Request.Body=http.MaxBytesReader(c.Writer,c.Request.Body,8192)
 if c.ContentType()!="application/json"{c.AbortWithStatus(http.StatusUnsupportedMediaType);return}
 var input struct{Servers []uint64 `json:"servers"`;Digest string `json:"digest"`;Canary string `json:"canary"`}
 d:=json.NewDecoder(c.Request.Body);d.DisallowUnknownFields()
 if err:=d.Decode(&input);err!=nil{c.AbortWithStatus(http.StatusBadRequest);return}
 if err:=d.Decode(new(any));err!=io.EOF{c.AbortWithStatus(http.StatusBadRequest);return}
 ctx,cancel:=context.WithTimeout(c.Request.Context(),30*time.Second)
 defer cancel()
 if err:=control.Scheduler.ExpireUnstarted(ctx);err!=nil{c.AbortWithStatus(http.StatusConflict);return}
 rows,err:=control.Scheduler.AdmitWave(ctx,id,input.Servers,input.Digest,input.Canary)
 if err!=nil{c.JSON(http.StatusConflict,gin.H{"message":"部分或全部任务未获授权，请检查已创建任务","tasks":rows});return}
 c.JSON(http.StatusOK,gin.H{"tasks":rows})
}
func(ma *memberAPI) upgradeStatus(c *gin.Context){
 control,_,ok:=upgradeAccess(c)
 if !ok{return}
 r,err:=control.Scheduler.Status(c.Request.Context(),c.Param("task"))
 if err!=nil{c.AbortWithStatus(http.StatusNotFound);return}
 c.JSON(http.StatusOK,r)
}

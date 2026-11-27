package controller
import("context";"encoding/json";"io";"net/http";"time";"github.com/gin-gonic/gin")
func(ma *memberAPI) upgradeURL(c *gin.Context){
 control,id,ok:=upgradeAccess(c);if !ok{return}
 if !control.Automatic(){c.AbortWithStatus(http.StatusServiceUnavailable);return}
 c.Request.Body=http.MaxBytesReader(c.Writer,c.Request.Body,8192)
 if c.ContentType()!="application/json"{c.AbortWithStatus(http.StatusUnsupportedMediaType);return}
 var in struct{Servers []uint64 `json:"servers"`;URL string `json:"url"`}
 dec:=json.NewDecoder(c.Request.Body);dec.DisallowUnknownFields()
 if err:=dec.Decode(&in);err!=nil{c.AbortWithStatus(http.StatusBadRequest);return}
 if err:=dec.Decode(new(any));err!=io.EOF{c.AbortWithStatus(http.StatusBadRequest);return}
 ctx,cancel:=context.WithTimeout(c.Request.Context(),3*time.Minute);defer cancel()
 tasks,err:=control.UpgradeURL(ctx,id,in.Servers,in.URL)
 if err!=nil{c.JSON(http.StatusConflict,gin.H{"message":"升级未全部创建：检查链接、版本、在线状态和首次登记","tasks":tasks});return}
 c.JSON(http.StatusOK,gin.H{"tasks":tasks})
}

package controller

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/url"
	"strings"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/hashicorp/go-uuid"
	"github.com/jinzhu/copier"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/sync/singleflight"

	"github.com/naiba/nezha/model"
	"github.com/naiba/nezha/pkg/mygin"
	"github.com/naiba/nezha/pkg/utils"
	"github.com/naiba/nezha/pkg/websocketx"
	"github.com/naiba/nezha/proto"
	"github.com/naiba/nezha/service/rpc"
	"github.com/naiba/nezha/service/singleton"
)

type commonPage struct {
	r            *gin.Engine
	requestGroup singleflight.Group
}

func (cp *commonPage) serve() {
	cr := cp.r.Group("")
	cr.Use(mygin.Authorize(mygin.AuthorizeOption{}))
	cr.Use(mygin.PreferredTheme)
	cr.POST("/view-password", cp.issueViewPassword)
	cr.GET("/terminal/:id", cp.terminal)
	cr.Use(mygin.ValidateViewPassword(mygin.ValidateViewPasswordOption{
		IsPage:        true,
		AbortWhenFail: true,
	}))
	cr.GET("/", cp.home)
	cr.GET("/service", cp.service)
	// TODO: 界面直接跳转使用该接口
	cr.GET("/network/:id", cp.network)
	cr.GET("/network", cp.network)
	cr.GET("/ws", cp.ws)
	cr.POST("/terminal", requireSameOriginMutation, cp.createTerminal)
	cr.POST("/file", requireSameOriginMutation, cp.createFM)
	cr.GET("/file/:id", cp.fm)
}

type viewPasswordForm struct {
	Password string
}

func (p *commonPage) issueViewPassword(c *gin.Context) {
	if !localSameOrigin(c) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer,c.Request.Body,16<<10)
	var vpf viewPasswordForm
	err := c.ShouldBind(&vpf)
	password := singleton.Conf.Site.ViewPassword
	if password == "" { c.Redirect(http.StatusSeeOther,"/"); return }
	key := "view:" + localRemoteIP(c)
	if !localReserveAttempt(key) { c.AbortWithStatus(http.StatusTooManyRequests); return }
	got,want := sha256.Sum256([]byte(vpf.Password)), sha256.Sum256([]byte(password))
	if err != nil || subtle.ConstantTimeCompare(got[:],want[:])!=1 {
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code: http.StatusOK,
			Title: singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID:"AnErrorEccurred"}),
			Msg: singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID:"WrongAccessPassword"}),
		},true)
		return
	}
	localResetAttempts(key)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(singleton.Conf.Site.CookieName+"-vp",mygin.NewViewPasswordToken(password,time.Now()),
		int(mygin.ViewPasswordAge.Seconds()),"/","",localSecureCookie(c),true)
	target := "/"
	if u,err:=url.Parse(c.Request.Referer());err==nil && u.Host==c.Request.Host && u.User==nil &&
		strings.HasPrefix(u.Path,"/") && !strings.HasPrefix(u.Path,"//") && !strings.Contains(u.Path,"\\") {
		target=u.RequestURI()
	}
	c.Redirect(http.StatusSeeOther,target)
}

func (p *commonPage) service(c *gin.Context) {
	res, _, _ := p.requestGroup.Do("servicePage", func() (interface{}, error) {
		singleton.AlertsLock.RLock()
		defer singleton.AlertsLock.RUnlock()
		stats := make(map[uint64]model.ServiceItemResponse)
		statsStore := make(map[uint64]model.CycleTransferStats)
		copier.Copy(&stats, singleton.ServiceSentinelShared.LoadStats())
		copier.Copy(&statsStore, singleton.AlertsCycleTransferStatsStore)
		if stats == nil { stats = make(map[uint64]model.ServiceItemResponse) }
		if statsStore == nil { statsStore = make(map[uint64]model.CycleTransferStats) }
		for k, service := range stats {
			if service.Monitor == nil || !service.Monitor.EnableShowInService {
				delete(stats, k)
			}
		}
		return []interface {
		}{
			stats, statsStore,
		}, nil
	})
	c.HTML(http.StatusOK, mygin.GetPreferredTheme(c, "/service"), mygin.CommonEnvironment(c, gin.H{
		"Title":              singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{MessageID: "ServicesStatus"}),
		"Services":           res.([]interface{})[0],
		"CycleTransferStats": res.([]interface{})[1],
	}))
}

func canViewServer(c *gin.Context,id uint64) bool {
	_, member:=c.Get(model.CtxKeyAuthorizedUser)
	_, verified:=c.Get(model.CtxKeyViewPasswordVerified)
	singleton.ServerLock.RLock()
	defer singleton.ServerLock.RUnlock()
	s:=singleton.ServerList[id]
	return s!=nil && (member || verified || !s.HideForGuest)
}

func (cp *commonPage) network(c *gin.Context) {
	var id uint64
	if raw:=c.Param("id");raw!="" {
		var err error
		id,err=strconv.ParseUint(raw,10,64)
		if err!=nil || id==0 || !canViewServer(c,id) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
	}
	var historyIDs []uint64
	if err:=singleton.DB.Model(&model.MonitorHistory{}).Distinct("server_id").
		Where("monitor_id != 0 AND server_id != 0").Pluck("server_id",&historyIDs).Error;err!=nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	haveHistory:=make(map[uint64]bool,len(historyIDs))
	for _,hid:=range historyIDs { haveHistory[hid]=true }
	_,member:=c.Get(model.CtxKeyAuthorizedUser)
	_,verified:=c.Get(model.CtxKeyViewPasswordVerified)
	servers:=make([]*model.Server,0)
	singleton.ServerLock.RLock()
	singleton.SortedServerLock.RLock()
	for _,entry:=range singleton.SortedServerList {
		if entry==nil { continue }
		server:=singleton.ServerList[entry.ID]
		if server==nil || (!member && !verified && server.HideForGuest) || !haveHistory[server.ID] {continue}
		item:=*server
		servers=append(servers,&item)
	}
	singleton.SortedServerLock.RUnlock()
	singleton.ServerLock.RUnlock()
	if id==0 && len(servers)>0 {id=servers[0].ID}
	if id==0 || !canViewServer(c,id) {
		mygin.ShowErrorPage(c,mygin.ErrInfo{Code:http.StatusForbidden,Title:"请求失败",
			Msg:"没有可查看的网络监控记录",Link:"/",Btn:"返回首页"},true)
		return
	}
	monitorInfos,_:=utils.Json.Marshal(singleton.MonitorAPI.GetMonitorHistories(map[string]any{"server_id":id}))
	serversBytes,_:=utils.Json.Marshal(Data{Now:time.Now().UnixMilli(),Servers:servers})
	c.HTML(http.StatusOK,mygin.GetPreferredTheme(c,"/network"),mygin.CommonEnvironment(c,gin.H{
		"Servers":string(serversBytes),"MonitorInfos":string(monitorInfos),"MaxTCPPingValue":singleton.Conf.MaxTCPPingValue,
	}))
}

func (cp *commonPage) getServerStat(c *gin.Context, withPublicNote bool) ([]byte, error) {
	_, isMember := c.Get(model.CtxKeyAuthorizedUser)
	_, isViewPasswordVerfied := c.Get(model.CtxKeyViewPasswordVerified)
	authorized := isMember || isViewPasswordVerfied
	v, err, _ := cp.requestGroup.Do(fmt.Sprintf("serverStats::%t::%t", authorized, withPublicNote), func() (interface{}, error) {
		singleton.SortedServerLock.RLock()
		defer singleton.SortedServerLock.RUnlock()

		var serverList []*model.Server
		if authorized {
			serverList = singleton.SortedServerList
		} else {
			serverList = singleton.SortedServerListForGuest
		}

		servers := make([]*model.Server, 0, len(serverList))
		for _, server := range serverList {
			if server == nil { continue }
			item := *server
			if !withPublicNote {
				item.PublicNote = ""
			}
			servers = append(servers, &item)
		}

		return utils.Json.Marshal(Data{
			Now:     time.Now().Unix() * 1000,
			Servers: servers,
		})
	})
	if err != nil { return nil, err }
	return v.([]byte), nil
}

func (cp *commonPage) home(c *gin.Context) {
	stat, err := cp.getServerStat(c, true)
	if err != nil {
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code: http.StatusInternalServerError,
			Title: singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{
				MessageID: "SystemError",
			}),
			Msg:  "服务器状态获取失败",
			Link: "/",
			Btn:  "返回首页",
		}, true)
		return
	}
	c.HTML(http.StatusOK, mygin.GetPreferredTheme(c, "/home"), mygin.CommonEnvironment(c, gin.H{
		"Servers": string(stat),
	}))
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  32768,
	WriteBufferSize: 32768,
}

type Data struct {
	Now     int64           `json:"now,omitempty"`
	Servers []*model.Server `json:"servers"`
}

var statsWSSlots = make(chan struct{}, 256)

func (cp *commonPage) ws(c *gin.Context) {
	select {
	case statsWSSlots <- struct{}{}: defer func() { <-statsWSSlots }()
	default: c.AbortWithStatus(http.StatusServiceUnavailable); return
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code: http.StatusInternalServerError,
			Title: singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{
				MessageID: "NetworkError",
			}),
			Msg:  "Websocket协议切换失败",
			Link: "/",
			Btn:  "返回首页",
		}, true)
		return
	}
	defer conn.Close()
	count := 0
	for {
		_ = conn.SetWriteDeadline(time.Now().Add(10*time.Second))
		stat, err := cp.getServerStat(c, false)
		if err != nil {
			return
		}
		if err := conn.WriteMessage(websocket.TextMessage, stat); err != nil {
			break
		}
		count += 1
		if count%4 == 0 {
			err = conn.WriteMessage(websocket.PingMessage, []byte{})
			if err != nil {
				break
			}
		}
		time.Sleep(time.Second * 2)
	}
}

func (cp *commonPage) terminal(c *gin.Context) {
	cp.connectBrowserStream(c, rpc.StreamTerminal)
}

func (cp *commonPage) connectBrowserStream(c *gin.Context, kind rpc.StreamKind) {
	value, ok := c.Get(model.CtxKeyAuthorizedUser)
	user, valid := value.(*model.User)
	id := c.Param("id")
	if !ok || !valid || user.ID == 0 || !rpc.NezhaHandlerSingleton.CanConnectUser(id, user.ID, kind) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	wsConn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil { return }
	defer wsConn.Close()
	conn := websocketx.NewConn(wsConn)
	if err := rpc.NezhaHandlerSingleton.UserConnected(id, user.ID, kind, conn); err != nil { return }
	defer rpc.NezhaHandlerSingleton.CloseStream(id)
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done: return
			case <-ticker.C:
				if conn.WriteMessage(websocket.PingMessage, nil) != nil { _ = conn.Close(); return }
			}
		}
	}()
	_ = rpc.NezhaHandlerSingleton.StartStream(id, 10*time.Second)
}

type createTerminalRequest struct {
	Host     string
	Protocol string
	ID       uint64
}

func (cp *commonPage) createTerminal(c *gin.Context) {
	if _, authorized := c.Get(model.CtxKeyAuthorizedUser); !authorized {
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code:  http.StatusForbidden,
			Title: "无权访问",
			Msg:   "用户未登录",
			Link:  "/login",
			Btn:   "去登录",
		}, true)
		return
	}
	var createTerminalReq createTerminalRequest
	if err := c.ShouldBind(&createTerminalReq); err != nil {
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code:  http.StatusForbidden,
			Title: "请求失败",
			Msg:   "请求参数有误：" + err.Error(),
			Link:  "/server",
			Btn:   "返回重试",
		}, true)
		return
	}

	streamId, err := uuid.GenerateUUID()
	if err != nil {
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code: http.StatusInternalServerError,
			Title: singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{
				MessageID: "SystemError",
			}),
			Msg:  "生成会话ID失败",
			Link: "/server",
			Btn:  "返回重试",
		}, true)
		return
	}

	server := singleton.SnapshotServer(createTerminalReq.ID)
	if server == nil || server.TaskStream == nil {
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code:  http.StatusForbidden,
			Title: "请求失败",
			Msg:   "服务器不存在或处于离线状态",
			Link:  "/server",
			Btn:   "返回重试",
		}, true)
		return
	}

	user := c.MustGet(model.CtxKeyAuthorizedUser).(*model.User)
	if err := rpc.NezhaHandlerSingleton.CreateStream(streamId, user.ID, server.ID, rpc.StreamTerminal); err != nil {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	terminalData, _ := utils.Json.Marshal(&model.TerminalTask{
		StreamID: streamId,
	})
	if err := server.TaskStream.Send(&proto.Task{
		Type: model.TaskTypeTerminalGRPC,
		Data: string(terminalData),
	}); err != nil {
		_ = rpc.NezhaHandlerSingleton.CloseStream(streamId)
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code:  http.StatusForbidden,
			Title: "请求失败",
			Msg:   "Agent信令下发失败",
			Link:  "/server",
			Btn:   "返回重试",
		}, true)
		return
	}

	c.HTML(http.StatusOK, "dashboard-"+singleton.Conf.Site.DashboardTheme+"/terminal", mygin.CommonEnvironment(c, gin.H{
		"SessionID":  streamId,
		"ServerName": server.Name,
		"ServerID":   server.ID,
	}))
}

func (cp *commonPage) fm(c *gin.Context) {
	cp.connectBrowserStream(c, rpc.StreamFile)
}

func (cp *commonPage) createFM(c *gin.Context) {
	IdString := c.PostForm("id")
	if _, authorized := c.Get(model.CtxKeyAuthorizedUser); !authorized {
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code:  http.StatusForbidden,
			Title: "无权访问",
			Msg:   "用户未登录",
			Link:  "/login",
			Btn:   "去登录",
		}, true)
		return
	}

	streamId, err := uuid.GenerateUUID()
	if err != nil {
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code: http.StatusInternalServerError,
			Title: singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{
				MessageID: "SystemError",
			}),
			Msg:  "生成会话ID失败",
			Link: "/server",
			Btn:  "返回重试",
		}, true)
		return
	}

	serverId, err := strconv.ParseUint(IdString, 10, 64)
	if err != nil {
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code:  http.StatusForbidden,
			Title: "请求失败",
			Msg:   "请求参数有误：" + err.Error(),
			Link:  "/server",
			Btn:   "返回重试",
		}, true)
		return
	}

	server := singleton.SnapshotServer(serverId)
	if server == nil || server.TaskStream == nil {
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code:  http.StatusForbidden,
			Title: "请求失败",
			Msg:   "服务器不存在或处于离线状态",
			Link:  "/server",
			Btn:   "返回重试",
		}, true)
		return
	}

	user := c.MustGet(model.CtxKeyAuthorizedUser).(*model.User)
	if err := rpc.NezhaHandlerSingleton.CreateStream(streamId, user.ID, server.ID, rpc.StreamFile); err != nil {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	fmData, _ := utils.Json.Marshal(&model.TaskFM{
		StreamID: streamId,
	})
	if err := server.TaskStream.Send(&proto.Task{
		Type: model.TaskTypeFM,
		Data: string(fmData),
	}); err != nil {
		_ = rpc.NezhaHandlerSingleton.CloseStream(streamId)
		mygin.ShowErrorPage(c, mygin.ErrInfo{
			Code:  http.StatusForbidden,
			Title: "请求失败",
			Msg:   "Agent信令下发失败",
			Link:  "/server",
			Btn:   "返回重试",
		}, true)
		return
	}

	c.HTML(http.StatusOK, "dashboard-"+singleton.Conf.Site.DashboardTheme+"/file", mygin.CommonEnvironment(c, gin.H{
		"SessionID": streamId,
	}))
}

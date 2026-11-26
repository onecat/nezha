package rpc

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/naiba/nezha/pkg/ddns"
	"github.com/naiba/nezha/pkg/geoip"
	"github.com/naiba/nezha/pkg/grpcx"
	"github.com/naiba/nezha/pkg/utils"

	"github.com/jinzhu/copier"
	"github.com/nicksnyder/go-i18n/v2/i18n"

	"github.com/naiba/nezha/model"
	pb "github.com/naiba/nezha/proto"
	"github.com/naiba/nezha/service/singleton"
)

var NezhaHandlerSingleton *NezhaHandler

type NezhaHandler struct {
	Auth          *authHandler
	ioStreams     map[string]*ioStreamContext
	ioStreamMutex *sync.RWMutex
}

func NewNezhaHandler() *NezhaHandler {
	return &NezhaHandler{
		Auth:          &authHandler{},
		ioStreamMutex: new(sync.RWMutex),
		ioStreams:     make(map[string]*ioStreamContext),
	}
}

func (s *NezhaHandler) ReportTask(c context.Context, r *pb.TaskResult) (*pb.Receipt, error) {
	var err error
	var clientID uint64
	if clientID, err = s.Auth.Check(c); err != nil {
		return nil, err
	}
	if r == nil {
		return &pb.Receipt{Proced: false}, nil
	}
	switch r.GetType() {
	case model.TaskTypeCommand, model.TaskTypeHTTPGet, model.TaskTypeICMPPing, model.TaskTypeTCPPing:
		if !singleton.ConsumeTrackedTask(clientID, r.GetId(), r.GetType()) {
			return &pb.Receipt{Proced: false}, nil
		}
	default:
		// Existing agents may report results for non-monitored control tasks.
		// Acknowledge without passing these into the monitor or command pipeline.
		return &pb.Receipt{Proced: true}, nil
	}
	if r.GetType() == model.TaskTypeCommand {
		// 处理上报的计划任务
		singleton.CronLock.RLock()
		defer singleton.CronLock.RUnlock()
		cr := singleton.Crons[r.GetId()]
		if cr != nil {
			singleton.ServerLock.RLock()
			defer singleton.ServerLock.RUnlock()
			if singleton.ServerList[clientID] == nil {
				return &pb.Receipt{Proced: false}, nil
			}
			// 保存当前服务器状态信息
			curServer := model.Server{}
			copier.Copy(&curServer, singleton.ServerList[clientID])
			if cr.PushSuccessful && r.GetSuccessful() {
				singleton.SendNotification(cr.NotificationTag, fmt.Sprintf("[%s] %s, %s\n%s", singleton.Localizer.MustLocalize(
					&i18n.LocalizeConfig{
						MessageID: "ScheduledTaskExecutedSuccessfully",
					},
				), cr.Name, singleton.ServerList[clientID].Name, r.GetData()), nil, &curServer)
			}
			if !r.GetSuccessful() {
				singleton.SendNotification(cr.NotificationTag, fmt.Sprintf("[%s] %s, %s\n%s", singleton.Localizer.MustLocalize(
					&i18n.LocalizeConfig{
						MessageID: "ScheduledTaskExecutedFailed",
					},
				), cr.Name, singleton.ServerList[clientID].Name, r.GetData()), nil, &curServer)
			}
			singleton.DB.Model(cr).Updates(model.Cron{
				LastExecutedAt: time.Now().Add(time.Second * -1 * time.Duration(r.GetDelay())),
				LastResult:     r.GetSuccessful(),
			})
		}
	} else if model.IsServiceSentinelNeeded(r.GetType()) {
		singleton.ServiceSentinelShared.Dispatch(singleton.ReportData{
			Data:     r,
			Reporter: clientID,
		})
	}
	return &pb.Receipt{Proced: true}, nil
}

func (s *NezhaHandler) RequestTask(h *pb.Host, stream pb.NezhaService_RequestTaskServer) error {
	clientID, err := s.Auth.Check(stream.Context())
	if err != nil { return err }
	closeCh := make(chan error)
	singleton.ServerLock.Lock()
	singleton.SortedServerLock.Lock()
	server := singleton.ServerList[clientID]
	if server == nil {
		singleton.SortedServerLock.Unlock()
		singleton.ServerLock.Unlock()
		return fmt.Errorf("agent no longer exists")
	}
	if server.TaskClose != nil { close(server.TaskClose) }
	server.TaskStream = stream
	server.TaskClose = closeCh
	singleton.SortedServerLock.Unlock()
	singleton.ServerLock.Unlock()

	// Cancellation must release the RPC even if this Agent never reconnects.
	// Only the current stream may clear its slot; an old RPC cannot erase a
	// newer connection. Use the map entry in case server metadata was edited.
	defer func() {
		singleton.ServerLock.Lock()
		singleton.SortedServerLock.Lock()
		if current := singleton.ServerList[clientID]; current != nil && current.TaskClose == closeCh {
			current.TaskStream = nil
			current.TaskClose = nil
		}
		singleton.SortedServerLock.Unlock()
		singleton.ServerLock.Unlock()
	}()
	select {
	case err := <-closeCh: return err
	case <-stream.Context().Done(): return stream.Context().Err()
	}
}

func (s *NezhaHandler) ReportSystemState(c context.Context, r *pb.State) (*pb.Receipt, error) {
	var clientID uint64
	var err error
	if clientID, err = s.Auth.Check(c); err != nil {
		return nil, err
	}
	state := model.PB2State(r)
	singleton.ServerLock.RLock()
	defer singleton.ServerLock.RUnlock()
	singleton.ServerList[clientID].LastActive = time.Now()
	singleton.ServerList[clientID].State = &state

	// 应对 dashboard 重启的情况，如果从未记录过，先打点，等到小时时间点时入库
	if singleton.ServerList[clientID].PrevTransferInSnapshot == 0 || singleton.ServerList[clientID].PrevTransferOutSnapshot == 0 {
		singleton.ServerList[clientID].PrevTransferInSnapshot = int64(state.NetInTransfer)
		singleton.ServerList[clientID].PrevTransferOutSnapshot = int64(state.NetOutTransfer)
	}

	return &pb.Receipt{Proced: true}, nil
}

func (s *NezhaHandler) ReportSystemInfo(c context.Context, r *pb.Host) (*pb.Receipt, error) {
	var clientID uint64
	var err error
	if clientID, err = s.Auth.Check(c); err != nil {
		return nil, err
	}
	host := model.PB2Host(r)
	singleton.ServerLock.RLock()
	defer singleton.ServerLock.RUnlock()

	// 检查并更新DDNS
	if singleton.ServerList[clientID].EnableDDNS && host.IP != "" &&
		(singleton.ServerList[clientID].Host == nil || singleton.ServerList[clientID].Host.IP != host.IP) {
		ipv4, ipv6, _ := utils.SplitIPAddr(host.IP)
		providers, err := singleton.GetDDNSProvidersFromProfiles(singleton.ServerList[clientID].DDNSProfiles, &ddns.IP{Ipv4Addr: ipv4, Ipv6Addr: ipv6})
		if err == nil {
			for _, provider := range providers {
				go func(provider *ddns.Provider) {
					provider.UpdateDomain(context.Background())
				}(provider)
			}
		} else {
			log.Printf("NEZHA>> 获取DDNS配置时发生错误: %v", err)
		}
	}

	// 发送IP变动通知
	if singleton.ServerList[clientID].Host != nil && singleton.Conf.EnableIPChangeNotification &&
		((singleton.Conf.Cover == model.ConfigCoverAll && !singleton.Conf.IgnoredIPNotificationServerIDs[clientID]) ||
			(singleton.Conf.Cover == model.ConfigCoverIgnoreAll && singleton.Conf.IgnoredIPNotificationServerIDs[clientID])) &&
		singleton.ServerList[clientID].Host.IP != "" &&
		host.IP != "" &&
		singleton.ServerList[clientID].Host.IP != host.IP {

		singleton.SendNotification(singleton.Conf.IPChangeNotificationTag,
			fmt.Sprintf(
				"[%s] %s, %s => %s",
				singleton.Localizer.MustLocalize(&i18n.LocalizeConfig{
					MessageID: "IPChanged",
				}),
				singleton.ServerList[clientID].Name, singleton.IPDesensitize(singleton.ServerList[clientID].Host.IP),
				singleton.IPDesensitize(host.IP),
			),
			nil)
	}

	/**
	 * 这里的 singleton 中的数据都是关机前的旧数据
	 * 当 agent 重启时，bootTime 变大，agent 端会先上报 host 信息，然后上报 state 信息
	 * 这是可以借助上报顺序的空档，将停机前的流量统计数据标记下来，加到下一个小时的数据点上
	 */
	if singleton.ServerList[clientID].Host != nil && singleton.ServerList[clientID].Host.BootTime < host.BootTime {
		singleton.ServerList[clientID].PrevTransferInSnapshot = singleton.ServerList[clientID].PrevTransferInSnapshot - int64(singleton.ServerList[clientID].State.NetInTransfer)
		singleton.ServerList[clientID].PrevTransferOutSnapshot = singleton.ServerList[clientID].PrevTransferOutSnapshot - int64(singleton.ServerList[clientID].State.NetOutTransfer)
	}

	// 不要冲掉国家码
	if singleton.ServerList[clientID].Host != nil {
		host.CountryCode = singleton.ServerList[clientID].Host.CountryCode
	}

	singleton.ServerList[clientID].Host = &host
	return &pb.Receipt{Proced: true}, nil
}

func (s *NezhaHandler) IOStream(stream pb.NezhaService_IOStreamServer) error {
	clientID, err := s.Auth.Check(stream.Context())
	if err != nil { return err }
	id, err := stream.Recv()
	if err != nil { return err }
	if id == nil || len(id.Data) != 40 || id.Data[0] != 0xff || id.Data[1] != 0x05 || id.Data[2] != 0xff || id.Data[3] != 0x05 {
		return fmt.Errorf("invalid stream id")
	}
	streamId := string(id.Data[4:])
	iw := grpcx.NewIOStreamWrapper(stream)
	if err := s.AgentConnected(streamId, clientID, iw); err != nil { return err }
	defer s.CloseStream(streamId)
	iw.Wait()
	return nil
}

func (s *NezhaHandler) LookupGeoIP(c context.Context, r *pb.GeoIP) (*pb.GeoIP, error) {
	var clientID uint64
	var err error
	if clientID, err = s.Auth.Check(c); err != nil {
		return nil, err
	}

	// 根据内置数据库查询 IP 地理位置
	record := &geoip.IPInfo{}
	ip := r.GetIp()
	netIP := net.ParseIP(ip)
	location, err := geoip.Lookup(netIP, record)
	if err != nil {
		return nil, err
	}

	// 将地区码写入到 Host
	singleton.ServerLock.RLock()
	defer singleton.ServerLock.RUnlock()
	if singleton.ServerList[clientID].Host == nil {
		return nil, fmt.Errorf("host not found")
	}
	singleton.ServerList[clientID].Host.CountryCode = location

	return &pb.GeoIP{Ip: ip, CountryCode: location}, nil
}

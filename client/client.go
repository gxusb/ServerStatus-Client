package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/shirou/gopsutil/load"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const Version = 0.17
const DefaultServer = "127.0.0.1"
const DefaultPort = "35601"
const DefaultInterval = 3
const DefaultUsername = "s01"
const DefaultPassword = "USER_DEFAULT_PASSWORD"
const DefaultProtocol = "ip4"
const PingPacketHistoryLen = 100
const TimeOut = time.Second * 3

const PingCu = "cu.tz.vizan.cc"
const PingCt = "ct.tz.vizan.cc"
const PingCm = "cm.tz.vizan.cc"

type Client struct {
	Server    string
	Port      string
	Username  string
	Password  string
	Interval  uint64
	Protocol  string
	Debug     bool
	waitGroup sync.WaitGroup
	conn      net.Conn
	lastTime  time.Time
	pingTime  sync.Map
	baseInfo  struct {
		checkIp uint8
		timer   uint8
	}
}

func (c *Client) Start() {
	go c.startPing()
	go c.startNet()
	go c.startDiskIo()
	go c.startRun()
}
func (c *Client) startRun() {
	defer func(conn net.Conn) {
		_ = conn.Close()

	}(c.conn)

	for range time.Tick(time.Second * time.Duration(c.Interval)) {
		var start = time.Now()
		var update = c.getUpdateInfo()
		if c.Debug {
			log.Printf("获取耗时：%v", time.Now().Sub(start))
		}
		var data = []byte("update ")
		if jsonByte, err := json.Marshal(update); err != nil {
			log.Println(err.Error())
		} else {
			_ = c.conn.SetWriteDeadline(time.Now().Add(TimeOut))
			data = append(data, jsonByte...)
			data = append(data, []byte("\n")...)
			write, err := c.conn.Write(data)
			if err != nil {
				_ = c.conn.Close()
				log.Printf("[准备重连]发送失败：%s\n", err.Error())

				if err = c.Conn(); err != nil {

					log.Printf("服务器重连失败：%s\n", err.Error())
				}
			} else {
				log.Printf("发送成功：%dByte\n", write)
			}
		}
	}
}
func (c *Client) Conn() error {
	var recvData = make([]byte, 128)
	var addr = fmt.Sprintf("%s:%v", c.Server, c.Port)

	conn, err := net.DialTimeout("tcp", addr, TimeOut)
	if err != nil {

		return errors.New(fmt.Sprintf("[连接]建立失败：%s", err.Error()))
	}

	for {
		_ = conn.SetReadDeadline(time.Now().Add(TimeOut))
		_ = conn.SetWriteDeadline(time.Now().Add(TimeOut))
		_, err = conn.Read(recvData)
		if err != nil {

			return errors.New(fmt.Sprintf("[连接]数据响应错误：%s，%s", err.Error(), string(recvData)))
		}

		if strings.Contains(string(recvData), "Authentication required") {
			_, err := conn.Write([]byte(fmt.Sprintf("%s:%s\n", c.Username, c.Password)))
			if err != nil {

				return errors.New(fmt.Sprintf("[连接]数据发送失败：%s", err.Error()))
			}

			continue
		}

		if strings.Contains(string(recvData), "You are connecting via") {
			if !strings.Contains(string(recvData), "IPv4") {
				c.baseInfo.checkIp = 4
			} else {
				c.baseInfo.checkIp = 6
			}

			break
		}
	}

	c.conn = conn

	log.Println("服务器连接成功")

	return nil
}
func (c *Client) getUpdateInfo() update {
	c.waitGroup = sync.WaitGroup{}
	var ret = &update{}

	c.waitGroup.Add(1)
	go c.getUpTime(ret)

	c.waitGroup.Add(1)
	go c.getCpuPercent(ret)

	c.waitGroup.Add(1)
	go c.getMemory(ret)

	c.waitGroup.Add(1)
	go c.getSwap(ret)

	c.waitGroup.Add(1)
	go c.getDiskUsage(ret)

	c.waitGroup.Add(1)
	go c.getTraffic(ret)

	c.waitGroup.Add(1)
	go c.GetNetRate(ret)

	ret.PingCM = c.getLostPacket("cm")
	ret.PingCU = c.getLostPacket("cu")
	ret.PingCT = c.getLostPacket("ct")

	ret.TimeCM = c.getPingTime("cm")
	ret.TimeCU = c.getPingTime("cu")
	ret.TimeCT = c.getPingTime("ct")

	c.waitGroup.Add(1)
	go c.getDiskIo(ret)

	c.waitGroup.Add(1)
	go c.getTupd(ret)

	if loadavg, err := load.Avg(); err == nil {
		ret.Load1 = loadavg.Load1
		ret.Load5 = loadavg.Load5
		ret.Load15 = loadavg.Load15
	}

	ret.IpStatus = true
	c.waitGroup.Wait()

	return *ret
}

func NewClient(server, username, password, port, interval string, debug bool) (*Client, error) {
	c := Client{
		Server:   DefaultServer,
		Username: DefaultUsername,
		Password: DefaultPassword,
		Port:     DefaultPort,
		Interval: DefaultInterval,
	}

	if server != "" {

		c.Server = server
	}
	if username != "" {

		c.Username = username
	}
	if password != "" {

		c.Password = password
	}
	if port != "" {

		c.Port = port
	}
	if i, err := strconv.ParseUint(interval, 10, 64); err == nil && i != 0 {

		c.Interval = i
	}

	c.Debug = debug
	c.Protocol = DefaultProtocol
	c.pingTime = sync.Map{}

	if err := c.Conn(); err != nil {

		return nil, err
	}

	return &c, nil
}

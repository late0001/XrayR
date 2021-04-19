package sspanel

import (
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/late0001/XrayR/api"
	"github.com/go-resty/resty/v2"
)

var (
	firstPortRe   = regexp.MustCompile(`(?m)port=(?P<outport>\d+)#?`) // First Port
	secondPortRe  = regexp.MustCompile(`(?m)port=\d+#(\d+)`)          // Second Port
	hostRe        = regexp.MustCompile(`(?m)host=([\w\.]+)\|?`)       // Host
	enableXtlsRe  = regexp.MustCompile(`(?m)enable_xtls=(\w+)\|?`)    // EnableXtls
	enableVlessRe = regexp.MustCompile(`(?m)enable_vless=(\w+)\|?`)   // EnableVless

)

// APIClient create a api client to the panel.
type APIClient struct {
	client      *resty.Client
	APIHost     string
	NodeID      int
	Key         string
	NodeType    string
	EnableVless bool
	EnableXTLS  bool
}

// New creat a api instance
func New(apiConfig *api.Config) *APIClient {

	client := resty.New()
	client.SetRetryCount(3)
	if apiConfig.Timeout > 0 {
		client.SetTimeout(time.Duration(apiConfig.Timeout) * time.Second)
	} else {
		client.SetTimeout(5 * time.Second)
	}
	client.OnError(func(req *resty.Request, err error) {
		if v, ok := err.(*resty.ResponseError); ok {
			// v.Response contains the last response from the server
			// v.Err contains the original error
			log.Print(v.Err)
		}
	})
	client.SetHostURL(apiConfig.APIHost)
	// Create Key for each requests
	client.SetQueryParam("key", apiConfig.Key)
	apiClient := &APIClient{
		client:      client,
		NodeID:      apiConfig.NodeID,
		Key:         apiConfig.Key,
		APIHost:     apiConfig.APIHost,
		NodeType:    apiConfig.NodeType,
		EnableVless: apiConfig.EnableVless,
		EnableXTLS:  apiConfig.EnableXTLS,
	}
	return apiClient
}

// Describe return a description of the client
func (c *APIClient) Describe() api.ClientInfo {
	return api.ClientInfo{APIHost: c.APIHost, NodeID: c.NodeID, Key: c.Key, NodeType: c.NodeType}
}

// Debug set the client debug for client
func (c *APIClient) Debug() {
	c.client.SetDebug(true)
}

func (c *APIClient) assembleURL(path string) string {
	return c.APIHost + path
}

func (c *APIClient) parseResponse(res *resty.Response, path string, err error) (*Response, error) {
	if err != nil {
		return nil, fmt.Errorf("request %s failed: %s", c.assembleURL(path), err)
	}

	if res.StatusCode() > 400 {
		body := res.Body()
		return nil, fmt.Errorf("request %s failed: %s, %s", c.assembleURL(path), string(body), err)
	}
	response := res.Result().(*Response)

	if response.Ret != 1 {
		res, _ := json.Marshal(&response)
		return nil, fmt.Errorf("Ret %s invalid", string(res))
	}
	return response, nil
}

// GetNodeInfo will pull NodeInfo Config from sspanel
func (c *APIClient) GetNodeInfo() (nodeInfo *api.NodeInfo, err error) {
	path := fmt.Sprintf("/mod_mu/nodes/%d/info", c.NodeID)
	res, err := c.client.R().
		SetResult(&Response{}).
		ForceContentType("application/json").
		Get(path)

	response, err := c.parseResponse(res, path, err)
	if err != nil {
		return nil, err
	}

	nodeInfoResponse := new(NodeInfoResponse)

	if err := json.Unmarshal(response.Data, nodeInfoResponse); err != nil {
		return nil, fmt.Errorf("Unmarshal %s failed: %s", reflect.TypeOf(nodeInfoResponse), err)
	}
	//err = fmt.Errorf("jjjj Node type: %s", c.NodeType)
	//log.Print(err);
	switch c.NodeType {
	case "V2ray":
		nodeInfo, err = c.ParseV2rayNodeResponse(nodeInfoResponse)
	case "Trojan":
		nodeInfo, err = c.ParseTrojanNodeResponse(nodeInfoResponse)
	case "Shadowsocks":
		nodeInfo, err = c.ParseSSNodeResponse(nodeInfoResponse)
	default:
		return nil, fmt.Errorf("Unsupported Node type: %s", c.NodeType)
	}

	if err != nil {
		res, _ := json.Marshal(nodeInfoResponse)
		return nil, fmt.Errorf("Parse node info failed: %s", string(res))
	}

	return nodeInfo, nil
}

// GetUserList will pull user form sspanel
func (c *APIClient) GetUserList() (UserList *[]api.UserInfo, err error) {
	path := "/mod_mu/users"
	res, err := c.client.R().
		SetQueryParam("node_id", strconv.Itoa(c.NodeID)).
		SetResult(&Response{}).
		ForceContentType("application/json").
		Get(path)

	response, err := c.parseResponse(res, path, err)

	userListResponse := new([]UserResponse)

	if err := json.Unmarshal(response.Data, userListResponse); err != nil {
		return nil, fmt.Errorf("Unmarshal %s failed: %s", reflect.TypeOf(userListResponse), err)
	}
	userList, err := c.ParseUserListResponse(userListResponse)
	if err != nil {
		res, _ := json.Marshal(userListResponse)
		return nil, fmt.Errorf("Parse user list failed: %s", string(res))
	}
	return userList, nil
}

// ReportNodeStatus reports the node status to the sspanel
func (c *APIClient) ReportNodeStatus(nodeStatus *api.NodeStatus) (err error) {
	path := fmt.Sprintf("/mod_mu/nodes/%d/info", c.NodeID)
	systemload := SystemLoad{
		Uptime: strconv.Itoa(nodeStatus.Uptime),
		Load:   fmt.Sprintf("%.2f %.2f %.2f", nodeStatus.CPU/100, nodeStatus.CPU/100, nodeStatus.CPU/100),
	}

	res, err := c.client.R().
		SetBody(systemload).
		SetResult(&Response{}).
		ForceContentType("application/json").
		Post(path)

	_, err = c.parseResponse(res, path, err)
	if err != nil {
		return err
	}

	return nil
}

//ReportNodeOnlineUsers reports online user ip
func (c *APIClient) ReportNodeOnlineUsers(onlineUserList *[]api.OnlineUser) error {

	data := make([]OnlineUser, len(*onlineUserList))
	for i, user := range *onlineUserList {
		data[i] = OnlineUser{UID: user.UID, IP: user.IP}
	}
	postData := &PostData{Data: data}
	path := fmt.Sprintf("/mod_mu/users/aliveip")
	res, err := c.client.R().
		SetQueryParam("node_id", strconv.Itoa(c.NodeID)).
		SetBody(postData).
		SetResult(&Response{}).
		ForceContentType("application/json").
		Post(path)

	_, err = c.parseResponse(res, path, err)
	if err != nil {
		return err
	}

	return nil
}

// ReportUserTraffic reports the user traffic
func (c *APIClient) ReportUserTraffic(userTraffic *[]api.UserTraffic) error {

	data := make([]UserTraffic, len(*userTraffic))
	for i, traffic := range *userTraffic {
		data[i] = UserTraffic{
			UID:      traffic.UID,
			Upload:   traffic.Upload,
			Download: traffic.Download}
	}
	postData := &PostData{Data: data}
	path := "/mod_mu/users/traffic"
	res, err := c.client.R().
		SetQueryParam("node_id", strconv.Itoa(c.NodeID)).
		SetBody(postData).
		SetResult(&Response{}).
		ForceContentType("application/json").
		Post(path)
	_, err = c.parseResponse(res, path, err)
	if err != nil {
		return err
	}

	return nil
}

// GetNodeRule will pull the audit rule form sspanel
func (c *APIClient) GetNodeRule() (*[]api.DetectRule, error) {
	path := "mod_mu/func/detect_rules"
	res, err := c.client.R().
		SetResult(&Response{}).
		ForceContentType("application/json").
		Get(path)

	response, err := c.parseResponse(res, path, err)

	ruleListResponse := new([]RuleItem)

	if err := json.Unmarshal(response.Data, ruleListResponse); err != nil {
		return nil, fmt.Errorf("Unmarshal %s failed: %s", reflect.TypeOf(ruleListResponse), err)
	}
	ruleList := make([]api.DetectRule, len(*ruleListResponse))
	for i, r := range *ruleListResponse {
		ruleList[i] = api.DetectRule{
			ID:      r.ID,
			Pattern: r.Content,
		}
	}
	return &ruleList, nil
}

// ReportIllegal reports the user illegal behaviors
func (c *APIClient) ReportIllegal(detectResultList *[]api.DetectResult) error {

	data := make([]IllegalItem, len(*detectResultList))
	for i, r := range *detectResultList {
		data[i] = IllegalItem{
			ID:  r.RuleID,
			UID: r.UID,
		}
	}
	postData := &PostData{Data: data}
	path := "/mod_mu/users/detectlog"
	res, err := c.client.R().
		SetQueryParam("node_id", strconv.Itoa(c.NodeID)).
		SetBody(postData).
		SetResult(&Response{}).
		ForceContentType("application/json").
		Post(path)
	_, err = c.parseResponse(res, path, err)
	if err != nil {
		return err
	}
	return nil
}

// ParseV2rayNodeResponse parse the response for the given nodeinfor format
func (c *APIClient) ParseV2rayNodeResponse(nodeInfoResponse *NodeInfoResponse) (*api.NodeInfo, error) {
	var enableTLS, enableVless bool
	enableVless = c.EnableVless
	var path, host, TLStype, transportProtocol string
	if nodeInfoResponse.RawServerString == "" {
		//err := fmt.Errorf("jjjj No server info in response")
		//log.Print(err);
		return nil, fmt.Errorf("No server info in response")
	}
	//nodeInfo.RawServerString = strings.ToLower(nodeInfo.RawServerString)
	serverConf := strings.Split(nodeInfoResponse.RawServerString, ";")
	port, err := strconv.Atoi(serverConf[1])
	if err != nil {
		return nil, err
	}
	alterID, err := strconv.Atoi(serverConf[2])
	if err != nil {
		return nil, err
	}
	// Compatible with more node types config
	for _, value := range serverConf[3:5] {
		switch value {
		case "tls", "xtls":
			TLStype = value
			enableTLS = true
		default:
			if value != "" {
				transportProtocol = value
			}
		}
	}
	extraServerConf := strings.Split(serverConf[5], "|")

	for _, item := range extraServerConf {
		conf := strings.Split(item, "=")
		key := conf[0]
		if key == "" {
			continue
		}
		value := conf[1]
		switch key {
		case "path":
			rawPath := strings.Join(conf[1:], "=") // In case of the path strings contains the "="
			path = rawPath
		case "host":
			host = value
		case "enable_vless":
			{
				if value == "true" {
					enableVless = true
				}
			}
		}
	}
	speedlimit := (nodeInfoResponse.SpeedLimit * 1000000) / 8
	// Create GeneralNodeInfo
	nodeinfo := &api.NodeInfo{
		NodeType:          c.NodeType,
		NodeID:            c.NodeID,
		Port:              port,
		SpeedLimit:        speedlimit,
		AlterID:           alterID,
		TransportProtocol: transportProtocol,
		EnableTLS:         enableTLS,
		TLSType:           TLStype,
		Path:              path,
		Host:              host,
		EnableVless:       enableVless,
	}

	return nodeinfo, nil
}

// ParseSSNodeResponse parse the response for the given nodeinfor format
func (c *APIClient) ParseSSNodeResponse(nodeInfoResponse *NodeInfoResponse) (*api.NodeInfo, error) {
	var port int = 0
	var method string
	path := "/mod_mu/users"
	res, err := c.client.R().
		SetQueryParam("node_id", strconv.Itoa(c.NodeID)).
		SetResult(&Response{}).
		ForceContentType("application/json").
		Get(path)

	response, err := c.parseResponse(res, path, err)

	userListResponse := new([]UserResponse)

	if err := json.Unmarshal(response.Data, userListResponse); err != nil {
		return nil, fmt.Errorf("Unmarshal %s failed: %s", reflect.TypeOf(userListResponse), err)
	}
	// Find the multi-user
	for _, u := range *userListResponse {
		if u.MultiUser > 0 {
			port = u.Port
			method = u.Method
			break
		}
	}

	if port == 0 || method == "" {
		return nil, fmt.Errorf("Cant find the single port multi user")
	}

	speedlimit := (nodeInfoResponse.SpeedLimit * 1000000) / 8
	// Create GeneralNodeInfo
	nodeinfo := &api.NodeInfo{
		NodeType:          c.NodeType,
		NodeID:            c.NodeID,
		Port:              port,
		SpeedLimit:        speedlimit,
		TransportProtocol: "tcp",
		CypherMethod:      method,
	}

	return nodeinfo, nil
}

// ParseTrojanNodeResponse parse the response for the given nodeinfor format
func (c *APIClient) ParseTrojanNodeResponse(nodeInfoResponse *NodeInfoResponse) (*api.NodeInfo, error) {
	// 域名或IP;port=连接端口#偏移端口|host=xx
	// gz.aaa.com;port=443#12345|host=hk.aaa.com
	var p, TLSType, host, enableXtls, outsidePort, insidePort string
	TLSType = "tls"
	if nodeInfoResponse.RawServerString == "" {
		return nil, fmt.Errorf("No server info in response")
	}
	if result := firstPortRe.FindStringSubmatch(nodeInfoResponse.RawServerString); len(result) > 1 {
		outsidePort = result[1]
	}
	if result := secondPortRe.FindStringSubmatch(nodeInfoResponse.RawServerString); len(result) > 1 {
		insidePort = result[1]
	}
	if result := hostRe.FindStringSubmatch(nodeInfoResponse.RawServerString); len(result) > 1 {
		host = result[1]
	}
	if result := enableXtlsRe.FindStringSubmatch(nodeInfoResponse.RawServerString); len(result) > 1 {
		enableXtls = result[1]
	}

	if insidePort != "" {
		p = insidePort
	} else {
		p = outsidePort
	}

	port, err := strconv.Atoi(p)
	if err != nil {
		return nil, err
	}
	if enableXtls == "true" {
		TLSType = "xtls"
	}
	speedlimit := (nodeInfoResponse.SpeedLimit * 1000000) / 8
	// Create GeneralNodeInfo
	nodeinfo := &api.NodeInfo{
		NodeType:          c.NodeType,
		NodeID:            c.NodeID,
		Port:              port,
		SpeedLimit:        speedlimit,
		TransportProtocol: "tcp",
		EnableTLS:         true,
		TLSType:           TLSType,
		Host:              host,
	}

	return nodeinfo, nil
}

// ParseUserListResponse parse the response for the given nodeinfor format
func (c *APIClient) ParseUserListResponse(userInfoResponse *[]UserResponse) (*[]api.UserInfo, error) {
	userList := make([]api.UserInfo, len(*userInfoResponse))
	for i, user := range *userInfoResponse {
		userList[i] = api.UserInfo{
			UID:           user.ID,
			Email:         user.Email,
			UUID:          user.UUID,
			Passwd:        user.Passwd,
			SpeedLimit:    (user.SpeedLimit * 1000000) / 8,
			DeviceLimit:   user.DeviceLimit,
			Port:          user.Port,
			Method:        user.Method,
			Protocol:      user.Protocol,
			ProtocolParam: user.ProtocolParam,
			Obfs:          user.Obfs,
			ObfsParam:     user.ObfsParam,
		}
	}

	return &userList, nil
}

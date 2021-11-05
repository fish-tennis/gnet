package gnet

// 连接方
type Connector struct {
	Connection
}

// 连接
func (this *Connector) Connect(address string) bool {
	return false
}

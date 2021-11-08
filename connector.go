package gnet

// 连接方
type Connector struct {
	baseConnection
}

// 连接
func (this *Connector) Connect(address string) bool {
	return false
}

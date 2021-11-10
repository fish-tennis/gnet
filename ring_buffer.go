package gnet

// 环形buffer,某些应用场景下,可以减少内存拷贝的次数
// NOTE:不支持多线程
type RingBuffer struct {
	// 数据
	buffer []byte
	// 写位置
	w int
	// 读位置
	r int
}

func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		return nil
	}
	return &RingBuffer{
		buffer: make([]byte, size),
	}
}

// 未被读取的长度
func (this *RingBuffer) UnReadLength() int {
	return this.w - this.r
}

// 读位置
func (this *RingBuffer) ReadIndex() int {
	return this.r%cap(this.buffer)
}

// 设置已读取长度
func (this *RingBuffer) SetReaded(readedLength int ) {
	this.r += readedLength
}

// 返回可读取buffer,没有copy
func (this *RingBuffer) ReadBuffer() []byte {
	readIndex := this.r%cap(this.buffer)
	if this.r%cap(this.buffer) < this.w%cap(this.buffer) {
		// 可读部分是连贯的
		return this.buffer[readIndex:readIndex+this.w-this.r]
	} else {
		// 可读部分被分割成尾部和头部两部分,先返回尾部那部分
		return this.buffer[readIndex:cap(this.buffer)]
	}
}

// 写入数据,如果没有足够空间可写,返回ErrBufferFull
func (this *RingBuffer) Write(p []byte) (n int, err error) {
	n = len(p)
	if n == 0 {
		return
	}
	bufferSize := cap(this.buffer)
	// 还有多少空间可以写
	sizeCanWrite := bufferSize+this.r-this.w
	if sizeCanWrite < len(p) {
		return 0, ErrBufferFull
	}
	writeIndex := this.w%bufferSize
	// [.......w..........]
	//         <- n ->
	// [.............w....] copy n
	if bufferSize-writeIndex >= n {
		// 数组尾部有足够的空间,则一次性拷贝
		copy(this.buffer[writeIndex:], p)
	} else {
		// [..........w..]
		//            <- n ->
		// [w............] copy (bufferSize-writeIndex)
		// [..w..........] copy n-(bufferSize-writeIndex)
		// 先拷贝一部分到数组尾部
		copy(this.buffer[writeIndex:], p[0:bufferSize-writeIndex])
		// 再拷贝剩下的部分到数组头部
		copy(this.buffer[0:], p[n-(bufferSize-writeIndex):])
	}
	this.w += n
	return
}
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

//// 读位置
//func (this *RingBuffer) ReadIndex() int {
//	return this.r%cap(this.buffer)
//}

// 设置已读取长度
func (this *RingBuffer) SetReaded(readedLength int ) {
	this.r += readedLength
}

// 返回可读取的连续buffer(不产生copy)
// NOTE:调用ReadBuffer之前,需要先确保UnReadLength()>0
func (this *RingBuffer) ReadBuffer() []byte {
	writeIndex := this.w%cap(this.buffer)
	readIndex := this.r%cap(this.buffer)
	if readIndex < writeIndex {
		// [_______r.....w_____]
		//         <- n ->
		// 可读部分是连续的
		return this.buffer[readIndex:readIndex+this.w-this.r]
	} else {
		// [........w_______r........]
		//                  <-  n1  ->
		// <-  n2  ->
		// 可读部分被分割成尾部和头部两部分,先返回尾部那部分
		return this.buffer[readIndex:]
	}
}

// 返回可写入的连续buffer
func (this *RingBuffer) WriteBuffer() []byte {
	writeIndex := this.w%cap(this.buffer)
	readIndex := this.r%cap(this.buffer)
	if readIndex < writeIndex {
		// [_______r.....w_____]
		// 可写部分被成尾部和头部两部分,先返回尾部那部分
		return this.buffer[writeIndex:]
	} else if readIndex > writeIndex {
		// [........w_______r........]
		// 可写部分是连续的
		return this.buffer[writeIndex:readIndex]
	} else {
		if this.r == this.w {
			return this.buffer[writeIndex:]
		}
		return nil
	}
}

// 设置已写入长度
func (this *RingBuffer) SetWrited(writedLength int ) {
	this.w += writedLength
}

// 写入数据
func (this *RingBuffer) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return
	}
	bufferSize := cap(this.buffer)
	canWriteSize := bufferSize + this.r - this.w
	if canWriteSize <= 0 {
		return 0, ErrBufferFull
	}
	writeIndex := this.w%bufferSize
	// 有足够的空间可以把p写完
	if canWriteSize >= len(p) {
		n = copy(this.buffer[writeIndex:], p)
		// 如果没能一次写完,说明写在尾部了,剩下的直接写入头部
		if n < len(p) {
			n += copy(this.buffer[0:], p[n:])
		}
		this.w += n
		return
	} else {
		n = copy(this.buffer[writeIndex:], p[0:canWriteSize])
		// 如果没能一次写完,说明写在尾部了,剩下的直接写入头部
		if n < canWriteSize {
			n += copy(this.buffer[0:], p[n:canWriteSize])
		}
		this.w += n
	}
	return
}
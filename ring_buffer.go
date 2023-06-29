package gnet

// 环形buffer,专为TcpConnection定制,在收发包时,可以减少内存分配和拷贝
// NOTE:不支持多线程,不具备通用性
//  optimize for TcpConnection, reduce memory alloc and copy
//  not thread safe
type RingBuffer struct {
	// 数据
	buffer []byte
	// 写位置
	//  write position
	w int
	// 读位置
	//  read position
	r int
}

// 指定大小的RingBuffer,不支持动态扩容
//  fix size, not support dynamic expansion
func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		return nil
	}
	return &RingBuffer{
		buffer: make([]byte, size),
	}
}

// Buffer
func (this *RingBuffer) GetBuffer() []byte {
	return this.buffer
}

func (this *RingBuffer) Size() int {
	return len(this.buffer)
}

// 未被读取的长度
func (this *RingBuffer) UnReadLength() int {
	return this.w - this.r
}

// 读取指定长度的数据
//  read data of a specified length
func (this *RingBuffer) ReadFull(readLen int) []byte {
	if this.UnReadLength() < readLen {
		return nil
	}
	readBuffer := this.ReadBuffer()
	if len(readBuffer) >= readLen {
		// 数据连续,不产生copy
		// dont copy with continuous data
		this.SetReaded(readLen)
		return readBuffer[0:readLen]
	} else {
		// 数据非连续,需要重新分配数组,并进行2次拷贝
		// data is not continuous, needs to be reassigned to the array and copied twice
		data := make([]byte, readLen)
		// 先拷贝RingBuffer的尾部
		// copy tail of RingBuffer first
		n := copy(data, readBuffer)
		// 再拷贝RingBuffer的头部
		// then copy head of RingBuffer
		copy(data[n:], this.buffer)
		this.SetReaded(readLen)
		return data
	}
}

// 设置已读取长度
//  set readed length
func (this *RingBuffer) SetReaded(readedLength int) {
	this.r += readedLength
}

// 返回可读取的连续buffer(不产生copy)
// NOTE:调用ReadBuffer之前,需要先确保UnReadLength()>0
//  continuous buffer can read
func (this *RingBuffer) ReadBuffer() []byte {
	writeIndex := this.w % len(this.buffer)
	readIndex := this.r % len(this.buffer)
	if readIndex < writeIndex {
		// [_______r.....w_____]
		//         <- n ->
		// 可读部分是连续的
		return this.buffer[readIndex : readIndex+this.w-this.r]
	} else {
		// [........w_______r........]
		//                  <-  n1  ->
		// <-  n2  ->
		// 可读部分被分割成尾部和头部两部分,先返回尾部那部分
		return this.buffer[readIndex:]
	}
}

// 返回可写入的连续buffer
//  continuous buffer can write
func (this *RingBuffer) WriteBuffer() []byte {
	writeIndex := this.w % len(this.buffer)
	readIndex := this.r % len(this.buffer)
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
//  set wrote length
func (this *RingBuffer) SetWrote(wroteLength int) {
	this.w += wroteLength
}

func (this *RingBuffer) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return
	}
	bufferSize := len(this.buffer)
	canWriteSize := bufferSize + this.r - this.w
	if canWriteSize <= 0 {
		return 0, ErrBufferFull
	}
	writeIndex := this.w % bufferSize
	// 有足够的空间可以把p写完
	// have enough space for p
	if canWriteSize >= len(p) {
		n = copy(this.buffer[writeIndex:], p)
		// 如果没能一次写完,说明写在尾部了,剩下的直接写入头部
		// if it cannot be written all at once, it means it is written at the tail,
		// and the rest is written directly at the head
		if n < len(p) {
			n += copy(this.buffer[0:], p[n:])
		}
		this.w += n
		return
	} else {
		n = copy(this.buffer[writeIndex:], p[0:canWriteSize])
		// 如果没能一次写完,说明写在尾部了,剩下的直接写入头部
		// if it cannot be written all at once, it means it is written at the tail,
		// and the rest is written directly at the head
		if n < canWriteSize {
			n += copy(this.buffer[0:], p[n:canWriteSize])
		}
		this.w += n
	}
	return
}

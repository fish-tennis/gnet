package gnet

import (
	"fmt"
	"testing"
)

func TestReadWrite(t *testing.T) {
	rb := NewRingBuffer(10)
	writeCounter := byte(0)
	for i := 0; i < 10; i++ {
		rb.Write([]byte{writeCounter})
		writeCounter++
	}
	println(fmt.Sprintf("%v", rb.buffer))
	println(fmt.Sprintf("readBuffer:%v", rb.ReadBuffer()))
	println(fmt.Sprintf("unReadLen:%v", rb.UnReadLength()))
	rb.SetReaded(8)
	for i := 0; i < 8; i++ {
		rb.Write([]byte{writeCounter})
		writeCounter++
	}
	println(fmt.Sprintf("%v", rb.buffer))
	println(fmt.Sprintf("readBuffer:%v", rb.ReadBuffer()))
	println(fmt.Sprintf("unReadLen:%v", rb.UnReadLength()))
	rb.SetReaded(len(rb.ReadBuffer()))
	println(fmt.Sprintf("readBuffer:%v", rb.ReadBuffer()))
	println(fmt.Sprintf("unReadLen:%v", rb.UnReadLength()))
}

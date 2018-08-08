package cmds

import (
	"context"
	"io"
	"sync"

	"github.com/ipfs/go-ipfs-cmdkit"
	"github.com/ipfs/go-ipfs-cmds/debug"
)

func NewChanResponsePair(req *Request) (ResponseEmitter, Response) {
	r := &chanResponse{
		req:     req,
		ch:      make(chan interface{}),
		waitLen: make(chan struct{}),
		closeCh: make(chan struct{}),
	}

	re := (*chanResponseEmitter)(r)

	return re, r
}

// chanStream is the struct of both the Response and ResponseEmitter.
// The methods are defined on chanResponse and chanResponseEmitter, which are
// just type definitions on chanStream.
type chanStream struct {
	req *Request

	// ch is used to send values from emitter to response.
	// When Emit received a channel close, it returns the error stored in err.
	ch chan interface{}

	// wl is a lock for writing calls, i.e. Emit, Close(WithError) and SetLength.
	wl sync.Mutex

	// closed stores whether this stream is closed.
	// It is protected by wl.
	closed bool

	// closeCh is closed when the stream is closed.
	// Error checks if the stream has been closed by checking if this channes is closed.
	// Its closing is protected by wl.
	closeCh chan struct{}

	// err is the error that the stream was closed with.
	// It is written once under lock wl, but only read after waitLen is closed (which also happens under wl)
	err error

	// waitLen is closed when the first value is emitted or the stream is closed.
	// Length waits for waitLen to be closed.
	// Its closing is protected by wl.
	waitLen chan struct{}

	// length is the length of the response.
	// It can be set by calling SetLength, but only before the first call to Emit, Close or CloseWithError.
	length uint64
}

type chanResponse chanStream

func (r *chanResponse) Request() *Request {
	return r.req
}

func (r *chanResponse) Error() *cmdkit.Error {
	select {
	case <-r.closeCh:
		if r.err == nil || r.err == io.EOF {
			return nil
		}

		if e, ok := r.err.(*cmdkit.Error); ok {
			return e
		}

		return &cmdkit.Error{Message: r.err.Error()}
	default:
		return nil
	}
}

func (r *chanResponse) Length() uint64 {
	<-r.waitLen

	return r.length
}

func (r *chanResponse) Next() (interface{}, error) {
	if r == nil {
		return nil, io.EOF
	}

	var ctx context.Context
	if rctx := r.req.Context; rctx != nil {
		ctx = rctx
	} else {
		ctx = context.Background()
	}

	select {
	case v, ok := <-r.ch:
		if !ok {
			return nil, r.err
		}

		switch val := v.(type) {
		case Single:
			return val.Value, nil
		default:
			return v, nil
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type chanResponseEmitter chanResponse

func (re *chanResponseEmitter) Emit(v interface{}) error {
	// channel emission iteration
	if ch, ok := v.(chan interface{}); ok {
		v = (<-chan interface{})(ch)
	}
	if ch, isChan := v.(<-chan interface{}); isChan {
		return EmitChan(re, ch)
	}

	re.wl.Lock()
	defer re.wl.Unlock()

	// Initially this library allowed commands to return errors by sending an
	// error value along a stream. We removed that in favour of CloseWithError,
	// so we want to make sure we catch situations where some code still uses the
	// old error emitting semantics and _panic_ in those situations.
	debug.AssertNotError(v)

	// unblock Length() and Error()
	select {
	case <-re.waitLen:
	default:
		close(re.waitLen)
	}

	// make sure we check whether the stream is closed *before accessing re.ch*!
	// re.ch is set to nil, but is not protected by a shared mutex (because that
	// wouldn't make sense).
	// re.closed is set in a critical section protected by re.wl (we also took
	// that lock), so we can be sure that this check is not racy.
	if re.closed {
		return ErrClosedEmitter
	}

	ctx := re.req.Context

	select {
	case re.ch <- v:
		if _, ok := v.(Single); ok {
			re.closeWithError(nil)
		}

		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (re *chanResponseEmitter) Close() error {
	return re.CloseWithError(nil)
}

func (re *chanResponseEmitter) SetLength(l uint64) {
	re.wl.Lock()
	defer re.wl.Unlock()

	// don't change value after emitting or closing
	select {
	case <-re.waitLen:
	default:
		re.length = l
	}
}

func (re *chanResponseEmitter) CloseWithError(err error) error {
	re.wl.Lock()
	defer re.wl.Unlock()

	if re.closed {
		return ErrClosingClosedEmitter
	}

	re.closeWithError(err)
	return nil
}

func (re *chanResponseEmitter) closeWithError(err error) {
	re.closed = true

	if err == nil {
		err = io.EOF
	}

	if e, ok := err.(cmdkit.Error); ok {
		err = &e
	}

	re.err = err
	close(re.ch)

	// unblock Length() and Error()
	select {
	case <-re.waitLen:
	default:
		close(re.waitLen)
	}
	select {
	case <-re.closeCh:
	default:
		close(re.closeCh)
	}
}

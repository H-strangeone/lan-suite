package transport

type QUICServer struct{}

func NewQUICServer() *QUICServer { return &QUICServer{} }

func (q *QUICServer) Start(addr string) error {

	return nil
}

func (q *QUICServer) Stop() {}

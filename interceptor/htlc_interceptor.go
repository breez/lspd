package interceptor

type HtlcInterceptor interface {
	Start() error
	Stop() error
	WaitStarted()
}

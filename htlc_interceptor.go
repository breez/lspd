package main

type HtlcInterceptor interface {
	Start() error
	Stop() error
	WaitStarted()
}

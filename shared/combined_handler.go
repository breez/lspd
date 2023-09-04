package shared

import "log"

type CombinedHandler struct {
	handlers []InterceptHandler
}

func NewCombinedHandler(handlers ...InterceptHandler) *CombinedHandler {
	return &CombinedHandler{
		handlers: handlers,
	}
}

func (c *CombinedHandler) Intercept(req InterceptRequest) InterceptResult {
	for i, handler := range c.handlers {
		res := handler.Intercept(req)
		log.Printf("Intercept %+v. Interceptor %d returns %+v", req, i, res)
		if res.Action != INTERCEPT_RESUME {
			return res
		}
	}

	return InterceptResult{
		Action: INTERCEPT_RESUME,
	}
}

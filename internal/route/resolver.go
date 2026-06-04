package route

func Resolve(routes []Route, reqHost, reqPath string) (*Route, bool) {
	for i := range routes {
		if MatchRoute(routes[i], reqHost, reqPath) {
			return &routes[i], true
		}
	}

	return nil, false
}

package midware

import (
	"log/slog"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// slowRequestThreshold marks requests worth logging even when they succeed.
const slowRequestThreshold = time.Second

// RequestLogger logs server errors and slow requests. Only the URL path is
// logged: query strings can carry password reset and confirmation tokens.
func RequestLogger(logger *slog.Logger) echo.MiddlewareFunc {
	return middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		HandleError: true,
		LogLatency:  true,
		LogMethod:   true,
		LogURIPath:  true,
		LogStatus:   true,
		LogError:    true,
		LogRemoteIP: true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			level := slog.LevelWarn
			switch {
			case v.Status >= 500:
				level = slog.LevelError
			case v.Latency < slowRequestThreshold:
				return nil
			}
			attributes := []slog.Attr{
				slog.String("method", v.Method),
				slog.String("path", v.URIPath),
				slog.Int("status", v.Status),
				slog.Duration("latency", v.Latency),
				slog.String("ip", v.RemoteIP),
			}
			if v.Error != nil {
				attributes = append(attributes, slog.String("error", v.Error.Error()))
			}
			logger.LogAttrs(c.Request().Context(), level, "request", attributes...)
			return nil
		},
	})
}

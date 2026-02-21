// Package middleware provides HTTP middleware for PingMe
package middleware

import (
	"strconv"
	"time"

	"PingMe/internal/metrics"

	"github.com/gin-gonic/gin"
)

// MetricsMiddleware collects HTTP metrics
func MetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.FullPath()
		if path == "" {
			path = "unknown"
		}
		
		c.Next()
		
		duration := time.Since(start).Seconds()
		status := strconv.Itoa(c.Writer.Status())
		method := c.Request.Method
		
		// Record metrics
		metrics.HTTPRequestsTotal.WithLabelValues(method, path, status).Inc()
		metrics.HTTPRequestDuration.WithLabelValues(method, path).Observe(duration)
	}
}

package router

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// SetupLocalAPI sets up a local HTTP API server for Wails fallback communication.
// Returns the gin engine and the http.Server so the caller can shut it down gracefully.
func SetupLocalAPI(port int, token string) (*gin.Engine, *http.Server) {
	r := gin.Default()

	// Middleware to verify token
	authMiddleware := func() gin.HandlerFunc {
		return func(c *gin.Context) {
			providedToken := c.GetHeader("X-API-Token")
			if providedToken != token {
				c.JSON(401, gin.H{"error": "unauthorized"})
				c.Abort()
				return
			}
			c.Next()
		}
	}

	api := r.Group("/api/v1", authMiddleware())
	{
		api.GET("/health", func(c *gin.Context) {
			c.JSON(200, gin.H{"status": "ok"})
		})
		// Additional API endpoints can be added here
	}

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	return r, srv
}

package auth

import "github.com/gin-gonic/gin"

// BasicAuth returns Gin middleware enforcing HTTP Basic Authentication
// with the given username/password pair.
func BasicAuth(username, password string) gin.HandlerFunc {
	accounts := gin.Accounts{username: password}
	return gin.BasicAuth(accounts)
}

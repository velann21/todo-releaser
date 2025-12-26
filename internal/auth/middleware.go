package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// IsAuthenticated is a middleware that checks if the user has already authenticated.
func IsAuthenticated(c *gin.Context) {
	session, _ := Store.Get(c.Request, "auth-session")
	if session.Values["id_token"] == nil {
		c.Redirect(http.StatusSeeOther, "/login")
		c.Abort()
		return
	}
	c.Next()
}

package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/velann21/todo-releaser/internal/auth"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, relying on environment variables")
	}

	auth.InitStore()

	authenticator, err := auth.NewAuthenticator()
	if err != nil {
		log.Fatalf("Failed to initialize authenticator: %v", err)
	}

	handler := auth.NewHandler(authenticator)

	r := gin.Default()

	r.GET("/login", handler.LoginHandler)
	r.GET("/callback", handler.CallbackHandler)
	r.GET("/logout", handler.LogoutHandler)

	r.GET("/user", auth.IsAuthenticated, func(c *gin.Context) {
		session, _ := auth.Store.Get(c.Request, "auth-session")
		c.JSON(http.StatusOK, gin.H{
			"id_token":     session.Values["id_token"],
			"access_token": session.Values["access_token"],
		})
	})

	log.Println("Server starting on :8080")
	if err := r.Run(":8080"); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

package main

import (
	"GoGinMoneyCopilot/database"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func main() {
	database.InitDB()
	r := gin.Default()

	r.POST("/accounts", func(c *gin.Context) {
		var input struct {
			Name   string `json:"name"`
			UserID int    `json:"user_id"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input format!"})
			return
		}
		if err := database.CreateAccount(input.Name, input.UserID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, gin.H{
			"message": "Account created!",
			"name":    input.Name,
		})
	})

	r.GET("/accounts/:id", func(c *gin.Context) {
		idParam := c.Param("id")
		id, err := strconv.Atoi(idParam)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID Format"})
			return
		}

		acc, err := database.GetAccount(id)
		if err != nil {
			if errors.Is(err, database.ErrAccountNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "Account not Found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, acc)
	})

	r.PUT("/accounts/:id", func(c *gin.Context) {
		idParam := c.Param("id")
		id, err := strconv.Atoi(idParam)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID Format"})
			return
		}

		var input struct {
			Name string `json:"name"`
		}
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input format"})
			return
		}

		if err := database.UpdateAccount(id, input.Name); err != nil {
			if errors.Is(err, database.ErrAccountNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "Account not Found!"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Account updated!",
		})
	})
    r.DELETE("/accounts/:id", func(c *gin.Context) {
        idParam := c.Param("id")
        id, err := strconv.Atoi(idParam)
        if err != nil {
            c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input Format"})
            return
        }
        
        if err := database.DeleteAccount(id); err != nil {
            if errors.Is(err, database.ErrAccountNotFound) {
                c.JSON(http.StatusNotFound, gin.H{"error": "Account not Found!"})
                return
            }
            c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
            return
        }
        c.JSON(http.StatusOK, gin.H{"message": "Account deleted!"})
    })

	r.Run(":8080")
}

//Framework ayağa kaldır
//Main döngüyü gör 
//Crud Çalıştır
//Authentication
//Jwoen token niye kullanıldı kullanılcak
//.env dosyasını kuracağız (db password) Sonra !
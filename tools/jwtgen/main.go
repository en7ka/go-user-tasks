package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func main() {
	sub := flag.Int64("sub", 0, "subject user id")
	secret := flag.String("secret", "dev-secret", "HS256 secret")
	role := flag.String("role", "", "optional role claim (e.g. admin)")
	ttl := flag.Duration("ttl", time.Hour*24, "token ttl")
	flag.Parse()

	claims := jwt.MapClaims{
		"sub": fmt.Sprintf("%d", *sub),
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(*ttl).Unix(),
	}
	if *role != "" {
		claims["role"] = *role
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := token.SignedString([]byte(*secret))
	if err != nil {
		panic(err)
	}
	fmt.Println(s)
}

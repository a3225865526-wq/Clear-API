package middleware

import (
	"net/http"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

type relayAdmission struct {
	mu                                          sync.Mutex
	active                                      int
	users, tokens                               map[int]int
	ips                                         map[string]int
	globalLimit, userLimit, tokenLimit, ipLimit int
}

func RelayConcurrencyLimit() gin.HandlerFunc {
	l := &relayAdmission{
		users: make(map[int]int), tokens: make(map[int]int), ips: make(map[string]int),
		globalLimit: max(1, common.GetEnvOrDefault("RELAY_CONCURRENCY_GLOBAL", 128)),
		userLimit:   max(1, common.GetEnvOrDefault("RELAY_CONCURRENCY_USER", 8)),
		tokenLimit:  max(1, common.GetEnvOrDefault("RELAY_CONCURRENCY_TOKEN", 4)),
		ipLimit:     max(1, common.GetEnvOrDefault("RELAY_CONCURRENCY_IP", 32)),
	}
	return func(c *gin.Context) {
		user, token, ip := c.GetInt("id"), c.GetInt("token_id"), c.ClientIP()
		l.mu.Lock()
		if l.active >= l.globalLimit || l.users[user] >= l.userLimit || token > 0 && l.tokens[token] >= l.tokenLimit || l.ips[ip] >= l.ipLimit {
			l.mu.Unlock()
			c.Header("Retry-After", "1")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": gin.H{"message": "Concurrent request limit exceeded", "type": "rate_limit_error"}})
			return
		}
		l.active++
		l.users[user]++
		l.ips[ip]++
		if token > 0 {
			l.tokens[token]++
		}
		l.mu.Unlock()
		defer func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.active--
			l.users[user]--
			l.ips[ip]--
			if l.users[user] == 0 {
				delete(l.users, user)
			}
			if l.ips[ip] == 0 {
				delete(l.ips, ip)
			}
			if token > 0 {
				l.tokens[token]--
				if l.tokens[token] == 0 {
					delete(l.tokens, token)
				}
			}
		}()
		c.Next()
	}
}

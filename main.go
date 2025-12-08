package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

var leaderboardUrl string
var windowsEndpoint string
var channelID string
var authEndpoint string
var ping string

var logger = log.New(os.Stdout, "[canny] ", log.LstdFlags)
var email = ""
var password = ""

type leaderboardEntry struct {
	Username         string    `json:"username"`
	TotalSubmissions int       `json:"total_submissions"`
	ChallengePoints  float64   `json:"challenge_points"`
	TotalPoints      float64   `json:"total_points"`
	LastSubmission   time.Time `json:"last_submission"`
}

type windowResponse struct {
	Day                int  `json:"day"`
	Enabled            bool `json:"enabled"`
	MaxSubmissions     int  `json:"max_submissions"`
	CurrentSubmissions int  `json:"current_submissions"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

func getAuthToken(username, password string) (string, error) {
	data := url.Values{}
	data.Set("username", username)
	data.Set("password", password)

	req, err := http.NewRequest("POST", authEndpoint, bytes.NewBufferString(data.Encode()))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "*/*")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var tokenResp tokenResponse
	err = json.Unmarshal(body, &tokenResp)
	if err != nil {
		return "", err
	}

	return tokenResp.AccessToken, nil
}

func getLeaderboard(token string) ([]leaderboardEntry, error) {
	req, err := http.NewRequest("GET", leaderboardUrl, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "*/*")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var leaderboard []leaderboardEntry
	err = json.Unmarshal(body, &leaderboard)
	if err != nil {
		return nil, err
	}

	return leaderboard, nil
}

func main() {
	if os.Getenv("ENV") == "testing" {
		godotenv.Load(".env.test")
	} else if os.Getenv("ENV") == "production" {
		godotenv.Load(".env.prod")
	}

	token := os.Getenv("TOKEN")
	if token == "" {
		logger.Fatal("TOKEN is not set")
	}

	email = os.Getenv("EMAIL")
	password = os.Getenv("PASSWORD")
	if email == "" || password == "" {
		logger.Fatal("EMAIL or PASSWORD is not set")
	}
	leaderboardUrl = os.Getenv("LEADERBOARDURL")
	if leaderboardUrl == "" {
		logger.Fatal("LEADERBOARDURL is not set")
	}
	windowsEndpoint = os.Getenv("WINDOWSURL")
	if windowsEndpoint == "" {
		logger.Fatal("WINDOWSURL is not set")
	}
	channelID = os.Getenv("CHANNELID")
	if channelID == "" {
		logger.Fatal("CHANNELID is not set")
	}
	authEndpoint = os.Getenv("AUTHURL")
	if authEndpoint == "" {
		logger.Fatal("AUTHURL is not set")
	}
	ping = os.Getenv("PING")
	if ping == "" {
		logger.Fatal("PING is not set")
	}

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		logger.Fatal("failed to create Discord session", err)
	}

	dg.Identify.Intents = discordgo.IntentsGuildMessages |
		discordgo.IntentsMessageContent |
		discordgo.IntentsDirectMessages

	dgErr := dg.Open()
	if dgErr != nil {
		logger.Fatal("failed to open Discord session", dgErr)
	}

	dg.AddHandler(messageCreate)

	logger.Println("Bot is now running.  Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	go pollWindows(dg)
	<-sc

	dg.Close()

}

func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}
	if m.Content == "!leaderboard" {
		token, err := getAuthToken(email, password)
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, "Error getting token")
			logger.Println("Error getting token", err)
			return
		}
		leaderboard, err := getLeaderboard(token)
		if err != nil {
			logger.Println("failed to fetch leaderboard:", err)
			s.ChannelMessageSend(m.ChannelID, "Failed to fetch leaderboard")
			return
		}

		response := formatLeaderboard(leaderboard)

		s.ChannelMessageSend(m.ChannelID, response)
	}

}

func formatLeaderboard(leaderboard []leaderboardEntry) string {
	if len(leaderboard) == 0 {
		return "No leaderboard data available."
	}

	var result strings.Builder
	result.WriteString("```\n")
	result.WriteString(fmt.Sprintf("%-3s %-20s %10s %8s %15s %10s\n", "Rank", "User", "Points", "Subs", "Last Submission", "Diff"))

	var fastestTime time.Time
	if len(leaderboard) > 0 {
		fastestTime = leaderboard[0].LastSubmission
	}

	for i, entry := range leaderboard {
		rank := fmt.Sprintf("%d.", i+1)
		relativeTime := getRelativeTime(entry.LastSubmission)

		var diffStr string
		if i == 0 {
			diffStr = "-"
		} else {
			diff := entry.LastSubmission.Sub(fastestTime)
			diffStr = formatTimeDiff(diff)
		}

		result.WriteString(fmt.Sprintf("%-3s %-20s %10.1f %8d %15s %10s\n",
			rank,
			truncate(entry.Username, 17),
			entry.TotalPoints,
			entry.TotalSubmissions,
			relativeTime,
			diffStr,
		))
	}

	result.WriteString("```")

	return result.String()
}

func truncate(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}

func getRelativeTime(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	if diff < time.Minute {
		return "now"
	} else if diff < time.Hour {
		mins := int(diff.Minutes())
		return fmt.Sprintf("%dm ago", mins)
	} else if diff < 24*time.Hour {
		hours := int(diff.Hours())
		return fmt.Sprintf("%dh ago", hours)
	} else if diff < 7*24*time.Hour {
		days := int(diff.Hours() / 24)
		return fmt.Sprintf("%dd ago", days)
	}
	return t.Format("Jan 2")
}

func formatTimeDiff(d time.Duration) string {
	if d < time.Minute {
		secs := int(d.Seconds())
		return fmt.Sprintf("+%ds", secs)
	} else if d < time.Hour {
		mins := int(d.Minutes())
		return fmt.Sprintf("+%dm", mins)
	} else if d < 24*time.Hour {
		hours := int(d.Hours())
		return fmt.Sprintf("+%dh", hours)
	}
	days := int(d.Hours() / 24)
	return fmt.Sprintf("+%dd", days)
}

func pollWindows(s *discordgo.Session) {
	var day int
	token, err := getAuthToken(email, password)
	if err != nil {
		logger.Println("Error getting token", err)
		return
	}
	dat, err := getWindows(token)
	if err != nil {
		logger.Println("failed to fetch windows:", err)
		return
	}
	data := dat[0]
	day = data.Day
	s.ChannelMessageSend(channelID, fmt.Sprintf("%s Day %d: Max submissions: %d, Current submissions: %d", ping, data.Day, data.MaxSubmissions, data.CurrentSubmissions))
	logger.Println("Day", data.Day, "Max submissions:", data.MaxSubmissions, "Current submissions:", data.CurrentSubmissions)
	for {
		time.Sleep(time.Minute)
		token, err := getAuthToken(email, password)
		if err != nil {
			logger.Println("Error getting token", err)
			continue
		}
		windows, err := getWindows(token)
		window := windows[0]
		day = window.Day
		if err != nil {
			logger.Println("failed to fetch windows:", err)
			continue
		}

		logger.Println("Day", window.Day, "Max submissions:", window.MaxSubmissions, "Current submissions:", window.CurrentSubmissions)
		if day != window.Day {
			s.ChannelMessageSend(channelID, fmt.Sprintf("%s Day %d: Max submissions: %d, Current submissions: %d", ping, window.Day, window.MaxSubmissions, window.CurrentSubmissions))
			logger.Println("Day", window.Day, "Max submissions:", window.MaxSubmissions, "Current submissions:", window.CurrentSubmissions)
			day = window.Day
			continue
		}
	}
}

func getWindows(token string) ([]windowResponse, error) {
	var windows []windowResponse
	req, err := http.NewRequest("GET", windowsEndpoint, nil)
	if err != nil {
		return windows, err
	}

	req.Header.Set("Accept", "*/*")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return windows, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return windows, err
	}

	err = json.Unmarshal(body, &windows)
	if err != nil {
		return windows, err
	}

	return windows, nil
}

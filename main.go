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
var initalPing bool

var logger = log.New(os.Stdout, "[canny] ", log.LstdFlags)
var email = ""
var password = ""

const maxDiscordMessageLength = 1900

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
	initalPing = os.Getenv("INITIALPING") == "true"

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

		_, sendErr := s.ChannelMessageSend(m.ChannelID, response)
		if sendErr != nil {
			logger.Println("failed to send message:", sendErr)
		}
	}

}

func formatLeaderboard(leaderboard []leaderboardEntry) string {
	if len(leaderboard) == 0 {
		return "No leaderboard data available."
	}

	var result strings.Builder
	header := fmt.Sprintf("```\n%-3s %-20s %10s %8s %15s %10s %15s\n", "Rank", "User", "Points", "Subs", "Last Submission", "Diff", "Total Diff")
	result.WriteString(header)

	var fastestTime time.Time
	if len(leaderboard) > 0 {
		fastestTime = leaderboard[0].LastSubmission
	}

	var previousSubmission time.Time

	truncatedCount := 0

	for i, entry := range leaderboard {
		rank := fmt.Sprintf("%d.", i+1)
		relativeTime := getRelativeTime(entry.LastSubmission)

		isTimeValid := !entry.LastSubmission.IsZero() && entry.LastSubmission.After(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))

		var diffStr string
		if i == 0 || !isTimeValid || previousSubmission.IsZero() {
			diffStr = "-"
		} else {
			diff := entry.LastSubmission.Sub(previousSubmission)
			diffStr = formatTimeDiff(diff)
		}

		var totalDiffStr string
		if i == 0 || !isTimeValid {
			totalDiffStr = "-"
		} else {
			totalDiff := entry.LastSubmission.Sub(fastestTime)
			totalDiffStr = formatTimeDiff(totalDiff)
		}

		line := fmt.Sprintf("%-3s %-20s %10.1f %8d %15s %10s %15s\n",
			rank,
			truncate(entry.Username, 17),
			entry.TotalPoints,
			entry.TotalSubmissions,
			relativeTime,
			diffStr,
			totalDiffStr,
		)

		if result.Len()+len(line)+len("```") > maxDiscordMessageLength {
			truncatedCount = len(leaderboard) - i
			break
		}

		result.WriteString(line)

		if isTimeValid {
			previousSubmission = entry.LastSubmission
		}
	}

	result.WriteString("```")

	if truncatedCount > 0 {
		result.WriteString(fmt.Sprintf("\n... %d more user(s) not displayed due to message length limit.", truncatedCount))
	}

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
	if initalPing {
		_, sendInitalErr := s.ChannelMessageSend(channelID, fmt.Sprintf("%s Day %d: Max submissions: %d, Current submissions: %d", ping, data.Day, data.MaxSubmissions, data.CurrentSubmissions))
		if sendInitalErr != nil {
			logger.Println("failed to send message:", sendInitalErr)
		}
	}

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
		if err != nil {
			logger.Println("failed to fetch windows:", err)
			continue
		}

		logger.Println("Day", window.Day, "Max submissions:", window.MaxSubmissions, "Current submissions:", window.CurrentSubmissions)
		if day != window.Day {
			day = window.Day
			_, sendErr := s.ChannelMessageSend(channelID, fmt.Sprintf("%s Day %d: Max submissions: %d, Current submissions: %d", ping, day, window.MaxSubmissions, window.CurrentSubmissions))
			if sendErr != nil {
				logger.Println("failed to send message:", sendErr)
			}
			logger.Println("Day", window.Day, "Max submissions:", window.MaxSubmissions, "Current submissions:", window.CurrentSubmissions)
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

func formatTimeDiff(d time.Duration) string {
	if d < 0 {
		d = -d
	}

	days := int(d.Hours() / 24)
	d -= time.Duration(days) * 24 * time.Hour

	hours := int(d.Hours())
	d -= time.Duration(hours) * time.Hour

	mins := int(d.Minutes())
	d -= time.Duration(mins) * time.Minute

	secs := int(d.Seconds())

	parts := []string{}

	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if mins > 0 {
		parts = append(parts, fmt.Sprintf("%dm", mins))
	}
	if secs > 0 && len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%ds", secs))
	}

	if len(parts) == 0 {
		return "+0s"
	}

	return "+" + strings.Join(parts, " ")
}

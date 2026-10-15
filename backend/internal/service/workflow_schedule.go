package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/robfig/cron/v3"
)

var workflowCronParser = cron.NewParser(
	cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
)

func nextWorkflowScheduledAt(raw json.RawMessage, after time.Time) (time.Time, error) {
	var config struct {
		EverySeconds   int    `json:"everySeconds"`
		CronExpression string `json:"cronExpression"`
		TimeZone       string `json:"timeZone"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&config) != nil {
		return time.Time{}, errors.New("定时配置无效")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return time.Time{}, errors.New("定时配置无效")
	}
	config.CronExpression = strings.TrimSpace(config.CronExpression)
	config.TimeZone = strings.TrimSpace(config.TimeZone)
	hasInterval := config.EverySeconds != 0
	hasCron := config.CronExpression != "" || config.TimeZone != ""
	if hasInterval == hasCron {
		return time.Time{}, errors.New("定时配置须选择间隔或 Cron 表达式其中一种")
	}
	if hasInterval {
		if config.EverySeconds < 60 || config.EverySeconds > 86400 {
			return time.Time{}, errors.New("定时间隔无效")
		}
		return after.UTC().Add(time.Duration(config.EverySeconds) * time.Second), nil
	}
	if config.CronExpression == "" || config.TimeZone == "" {
		return time.Time{}, errors.New("Cron 表达式和时区为必填项")
	}
	if _, err := time.LoadLocation(config.TimeZone); err != nil {
		return time.Time{}, errors.New("定时时区无效")
	}
	schedule, err := workflowCronParser.Parse("CRON_TZ=" + config.TimeZone + " " + config.CronExpression)
	if err != nil {
		return time.Time{}, errors.New("Cron 表达式无效")
	}
	next := schedule.Next(after)
	if next.IsZero() {
		return time.Time{}, errors.New("Cron 表达式没有后续执行时间")
	}
	return next.UTC(), nil
}

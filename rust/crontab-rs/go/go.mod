module rust-anything.local/crontab_rs

go 1.26.2

require github.com/alexgorbatchev/cron-person-cli v0.0.0

require (
	github.com/lnquy/cron v1.1.1 // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
	golang.org/x/sys v0.49.0 // indirect
)

replace github.com/alexgorbatchev/cron-person-cli => ../../..

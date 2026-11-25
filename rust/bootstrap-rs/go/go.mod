module rust-anything.local/bootstrap_rs

go 1.26.2

require github.com/alexgorbatchev/cron-person-cli v0.0.0

replace github.com/alexgorbatchev/cron-person-cli => ../../..

# goexpenses
Simple expenses web application written in Go, using Echo and PostgresSQL.
You must manualy execute script pg_structure.sql on Postgres databse first.
You can define expenses and incomes, and then enter posts.
It is possible to enter amounts in RSD or EUR currency.


Build and installation

Get source with

```
go get github.com/dkeza/goexpenses
```

Build binary

```
go build
```

Copy folders and files to install folder

```
db
static
templates
goexpenses.dev.ini
```

Rename goexpenses.dev.ini to goexpenses.ini, and enter settings.

Start binary

Database would be automatically created. EUR and RSD currency exchange rates would be automatically updated on start, and then once a day.
User must register with valid E-Mail. When reseting password, activation link is sent to E-Mail.

This example can be deployed to Heroku.

Working example on Heroku:
https://keza-goexpenses.herokuapp.com/

Credits to

* [Echo Web Framework](https://github.com/labstack/echo)

package util

type Data struct {
	User                  User
	Active                string
	CookieId              string
	Username              string
	Incomesum             string
	Incomesume            string
	Saldo                 string
	Saldoe                string
	Expensesum            string
	Expensesume           string
	Lang                  string
	Csrf                  string
	Token                 string
	Flash                 string
	Filter                string
	Eur                   float64
	Eurdate               string
	Expenses_id           string
	Last_post_description string
	Message_success       int
	Date                  string
	Accounts              []Account
	Posts                 []Post
	Expenses              []Expense
	ExpensesAdd           []Expense
	Incomes               []Income
}

// Account
type Account struct {
	Id          int    `db:"id"`
	Description string `db:"description"`
	Deleted     int    `db:"deleted"`
	Fromdate    string `db:"fromdate"`
	Todate      string `db:"todate"`
}

// Expense
type Expense struct {
	Id                 int     `db:"id"`
	Pid                string  `db:"p_id"`
	Description        string  `db:"description"`
	Amount             float64 `db:"amount"`
	Amounte            float64 `db:"amounte"`
	ExpensesId         int     `db:"expenses_id"`
	ExpensesPid        string  `db:"expenses_pid"`
	ExpenseDescription string  `db:"expensedescription"`
}

// Income
type Income struct {
	Id          int    `db:"id"`
	Pid         string `db:"p_id"`
	Description string `db:"description"`
}

type Post struct {
	Id          int     `db:"id"`
	Pid         string  `db:"p_id"`
	Description string  `db:"description"`
	Expense     string  `db:"expense"`
	Income      string  `db:"income"`
	Date        string  `db:"date"`
	Amount      float64 `db:"amount"`
	Amounte     float64 `db:"amounte"`
	Exchange    float64 `db:"exchange"`
}

type Session struct {
	Id                    int    `db:"id"`
	Uuid                  string `db:"uuid"`
	User_id               int    `db:"user_id"`
	Lang                  string `db:"lang"`
	Message               string `db:"message"`
	Expenses_id           int    `db:"expenses_id"`
	Last_post_description string `db:"last_post_description"`
	Message_success       int    `db:"message_success"`
	CreatedAt             string `db:"created_at"`
}

type Param struct {
	Id    int `db:"id"`
	Build int `db:"build"`
}

type User struct {
	Id                  int    `db:"id"`
	Name                string `db:"name"`
	Username            string `db:"username"`
	Email               string `db:"email"`
	Password            string `db:"password"`
	Default_accounts_id int    `db:"default_accounts_id"`
	Lang                string `db:"lang"`
}

type PasswordReset struct {
	Id        int    `db:"id"`
	Email     string `db:"email"`
	Token     string `db:"token"`
	Done      int    `db:"done"`
	CreatedAt string `db:"created_at"`
}

type Currency struct {
	Id   int     `db:"id"`
	Code string  `db:"code"`
	Rate float64 `db:"rate"`
	Date string  `db:"date"`
}

type Postsum struct {
	Saldo  float64
	Saldoe float64
}

type Incomessum struct {
	Saldo  float64
	Saldoe float64
}

type Expensessum struct {
	Saldo  float64
	Saldoe float64
}

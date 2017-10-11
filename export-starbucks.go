package main

import (
	"fmt"
	"github.com/dghubble/sling"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"net/http/cookiejar"
	"strings"
	"time"
	"encoding/csv"
	"reflect"
	"strconv"
)

var cookieJar, _ = cookiejar.New(nil)
var client = &http.Client{Jar: cookieJar, Timeout: 5 * time.Second}
var base = sling.New().Base("https://card.starbucks.ch").Client(client)

func main() {
	args := os.Args
	if len(args) < 3 {
		fmt.Println("Usage: starbucks-transaction-export <username/email> <password>")
		os.Exit(-1)
	}
	verificationToken := getVerificationToken("/login.aspx")

	email := args[1]
	password := args[2]
	login(verificationToken, email, password)

	// Logged in from this point on

	file, e := os.Create("starbucks-export.csv")
	if e != nil {
		log.Fatal(e)
	}
	defer file.Close()
	csvWriter := csv.NewWriter(file)

	writeCsvHeader(csvWriter)

	cardsResponse := getAllCards(verificationToken)
	for _, card := range cardsResponse.Cards {
		log.Printf("Found Card: Number %v, Active: %t, Balance: %v %v", card.Number, card.Active, card.Currency, card.Amount)

		transactionResponse := getAllTransactionsForCard(verificationToken, card)
		for _, transaction := range transactionResponse.Transactions {
			log.Printf("%+v", transaction)
			writeTransaction(csvWriter, card, transaction)
		}
	}

	csvWriter.Flush()
}

//region VerificationToken
type verificationToken struct {
	FormToken   string
	CookieToken string
}

func (token verificationToken) format() string {
	return token.CookieToken + ":" + token.FormToken
}

var formTokenRegex, _ = regexp.Compile("MSRService\\.FormToken\\s?=\\s?\"(.*)\";")
var cookieTokenRegex, _ = regexp.Compile("MSRService\\.CookieToken\\s?=\\s?\"(.*)\";")

func getVerificationToken(url string) verificationToken {
	request, e := base.New().Get(url).Request()
	if e != nil {
		log.Fatal(e)
	}

	response, _ := client.Do(request)
	defer response.Body.Close()

	bodyBytes, _ := ioutil.ReadAll(response.Body)
	bodyString := string(bodyBytes)
	formTokenMatches := formTokenRegex.FindStringSubmatch(bodyString)
	cookieTokenMatches := cookieTokenRegex.FindStringSubmatch(bodyString)

	return verificationToken{FormToken: formTokenMatches[1], CookieToken: cookieTokenMatches[1]}
}

//endregion

//region Login
type loginDataForm struct {
	LoginDataString string `url:"LoginDataString,omitempty"`
}

type loginResponseJson struct {
	Message string `json:"Message"`
}

func login(token verificationToken, email, password string) {
	loginDataString := fmt.Sprintf("Emailr0tn1L%sL1nt0rPasswordr0tn1L%sL1nt0rCaptchaTextr0tn1Lundefined", email, password)
	form := loginDataForm{LoginDataString: loginDataString}

	var loginResponse loginResponseJson
	response, _ := base.New().Set("RequestVerificationToken", token.format()).Post("/msrservice/Login?format=json").BodyForm(form).ReceiveSuccess(&loginResponse)
	defer response.Body.Close()

	if strings.Contains(loginResponse.Message, "Login False") {
		log.Fatal("Could not login!")
	}
	log.Println("Logged in successfully")
}

//endregion

//region Cards
type loadAllCardsDataResponse struct {
	Cards []card `json:"OldCardNumbers"`
}

type card struct {
	Number                       string  `json:"OldCardNumber"`
	TransferNumber               string  `json:"NewCardNumber,omitempty"`
	Active                       bool    `json:"IsActive"`
	Amount                       float32 `json:"Amount"`
	Stars                        int     `json:"Stars"`
	Currency                     string  `json:"Currency"`
	DataStringSeparator          string  `json:"DataStringSeparator"`
	PropertyValueStringSeparator string  `json:"PropertyValueStringSeparator"`
}

func getAllCards(token verificationToken) loadAllCardsDataResponse {
	var responseObj loadAllCardsDataResponse
	response, _ := base.New().Set("RequestVerificationToken", token.format()).Post("/msrservice/LoadAllCardsData?format=json").ReceiveSuccess(&responseObj)
	defer response.Body.Close()

	return responseObj
}

//endregion

//region Transactions
type transactionDetailForm struct {
	CardNumber       string `url:"CardNumber,omitempty"`
	SearchDataString string `url:"SearchDataString,omitempty"`
}

type transactionDetailResponse struct {
	Transactions []transaction `json:"ReturnValue"`
}

type transaction struct {
	Id                           int     `json:"TransactionID"`
	Category                     string  `json:"TransactionCategory"`
	MoneyAmount                  float64 `json:"Amount"`
	MoneyBalance                 float64 `json:"MoneyBalance"`
	StarAmount                   int     `json:"Stars"`
	StarBalance                  int     `json:"StarsBalance"`
	DateTimeUnix                 string  `json:"TransDateTime"`
	Description                  string  `json:"Description"`
	Points                       int     `json:"Points"`
	LocationId                   int     `json:"LocationId"`
	LocationName                 string  `json:"Location"`
	CheckNumber                  string  `json:"CheckNumber"`
	Currency                     string  `json:"Currency"`
	DataStringSeparator          string  `json:"DataStringSeparator"`
	PropertyValueStringSeparator string  `json:"PropertyValueStringSeparator"`
}

func getAllTransactionsForCard(token verificationToken, card card) transactionDetailResponse {
	var responseObj transactionDetailResponse

	form := transactionDetailForm{CardNumber: card.Number, SearchDataString: buildTransactionSearchDataString(card, 1, 150000)}
	response, _ := base.New().Set("RequestVerificationToken", token.format()).BodyForm(form).Post("/msrservice/TransactionDetail?format=json").ReceiveSuccess(&responseObj)
	defer response.Body.Close()

	return responseObj
}

func buildTransactionSearchDataString(card card, page, pageSize int) string {
	// page r0tn1L 1 L1nt0r page_size r0tn1L 150000
	return fmt.Sprintf("page%s%v%spage_size%s%v", card.PropertyValueStringSeparator, page, card.DataStringSeparator, card.PropertyValueStringSeparator, pageSize)
}

//endregion

//region CSV
func writeCsvHeader(writer *csv.Writer) {
	transactionValue := reflect.ValueOf(transaction{})
	fieldNames := make([]string, transactionValue.NumField()+1)
	fieldNames[0] = "CardNumber"

	for i := 0; i < transactionValue.NumField(); i++ {
		fieldNames[i+1] = transactionValue.Type().Field(i).Name
	}
	writer.Write(fieldNames[:cap(fieldNames)-2])
}

func writeTransaction(writer *csv.Writer, card card, transaction transaction) {
	record := make([]string, 14)
	record[0] = card.Number
	record[1] = strconv.Itoa(transaction.Id)
	record[2] = transaction.Category
	record[3] = strconv.FormatFloat(transaction.MoneyAmount, 'f', -1, 64)
	record[4] = strconv.FormatFloat(transaction.MoneyBalance, 'f', -1, 64)
	record[5] = strconv.Itoa(transaction.StarAmount)
	record[6] = strconv.Itoa(transaction.StarBalance)
	record[7] = formatWeirdUnixTime(transaction.DateTimeUnix)
	record[8] = transaction.Description
	record[9] = strconv.Itoa(transaction.Points)
	record[10] = strconv.Itoa(transaction.LocationId)
	record[11] = transaction.LocationName
	record[12] = transaction.CheckNumber
	record[13] = transaction.Currency

	writer.Write(record)
}

var unixDateRegex, _ = regexp.Compile("/Date\\((\\d+)\\d{3}-0000\\)/")

func formatWeirdUnixTime(unix string) string {
	submatches := unixDateRegex.FindStringSubmatch(unix)
	unixSeconds, _ := strconv.ParseInt(submatches[1], 10, 64)
	timeObj := time.Unix(unixSeconds, 0)

	return timeObj.Format(time.RFC3339)
}

//endregion

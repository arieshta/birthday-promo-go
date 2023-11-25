package main

import (
	"crypto"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/go-co-op/gocron"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/twilio/twilio-go"
	twilioApi "github.com/twilio/twilio-go/rest/api/v2010"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var DB *gorm.DB

const dateFormat = "2006-01-02"

func InitDB() {
	err := godotenv.Load(".env")
	if err != nil {
		log.Fatalf("Error loading environment variables")
		os.Exit(1)
	}

	connectionString := fmt.Sprintf("user=%s password=%s host=%s port=%s dbname=%s",
		os.Getenv("DB_username"),
		os.Getenv("DB_password"),
		os.Getenv("DB_host"),
		os.Getenv("DB_port"),
		os.Getenv("DB_name"))

	var e error
	DB, e = gorm.Open(postgres.Open(connectionString), &gorm.Config{})
	if e != nil {
		panic(e)
	}

	InitMigrate()
	if os.Getenv("DB_refresh") == "1" {
		InsertSeedUser()
	}
}

func InitMigrate() {
	if os.Getenv("DB_refresh") == "1" {
		DB.Migrator().DropTable(&User{})
		DB.Migrator().DropTable(&PromoCode{})
	}
	DB.AutoMigrate(&User{})
	DB.AutoMigrate(&PromoCode{})
}

func InsertSeedUser() {
	type Temp struct {
		Name           string `json:"name"`
		Email          string `json:"email"`
		Birthday       string `json:"birthday"`
		JoinedMinuteAt int    `json:"joined_minute_at"`
		VerifiedStatus bool   `json:"verified_status"`
		Phone          string `json:"phone"`
	}

	type Temps struct {
		Temps []Temp `json:"user"`
	}

	jsonfile, err := os.Open("./seeds/users.json")
	if err != nil {
		fmt.Println(err)
	}
	byteValue, err := io.ReadAll(jsonfile)
	if err != nil {
		fmt.Println(err)
	}

	var temp Temps
	json.Unmarshal(byteValue, &temp)

	for _, i := range temp.Temps {
		t, _ := time.Parse(dateFormat, i.Birthday)

		DB.Create(&User{
			Name:           i.Name,
			Email:          i.Email,
			Birthday:       t,
			JoinedMinuteAt: i.JoinedMinuteAt,
			VerifiedStatus: i.VerifiedStatus,
			Phone:          i.Phone,
		})
	}

	defer jsonfile.Close()
}

type User struct {
	ID             uuid.UUID `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	Name           string    `json:"name"`
	Email          string    `json:"email"`
	Birthday       time.Time `json:"birthday"`
	JoinedMinuteAt int       `json:"joined_minute_at"`
	VerifiedStatus bool      `json:"verified_status"`
	Phone          string    `json:"phone"`
}

type Users struct {
	Users []User `json:"user"`
}

type PromoCode struct {
	ID        uuid.UUID `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	UserId    uuid.UUID `json:"user_id"`
	Code      string    `json:"code"`
	StartDate time.Time `json:"start_date"`
	EndDate   time.Time `json:"end_date"`
	Amount    float64   `json:"amount"`
	User      User      `gorm:"foreignKey:UserId;references:ID" json:"user"`
}

type FetchUserQuery struct {
	Email          string
	VerifiedStatus bool
	IsBirthday     bool
}

type FetchVerifiedBirthdayUsersQuery struct {
	// Email          string
	VerifiedStatus bool
	IsBirthday     bool
}

type CreatePromoFields struct {
	Name        string
	StartDate   time.Time
	EndDate     time.Time
	Amount      float64
	ValidUserId string
}

type NotificationParams struct {
	NotificationType string
	Subject          string
	Body             string
	Target           string
}

func FetchUser(query FetchUserQuery) (User, error) {
	var user User

	querys := fmt.Sprintf("email = '%s' and verified_status = %t", query.Email, query.VerifiedStatus)

	if query.IsBirthday {
		testDate := "1998-07-17"
		// testDate := "1998-06-22"

		// now, _ := time.Parse("2006-01-02", time.Now().String())
		now, _ := time.Parse("2006-01-02", testDate)

		querys = fmt.Sprintf("%s and birthday = '%s'", querys, now.Format("2006-01-02 15:04:05+00"))
	}

	result := DB.Where(querys).First(&user)
	if result.Error != nil {
		return User{}, result.Error
	}
	return user, nil
}

func FetchUsers() ([]string, error) {
	type Email struct {
		Email string
	}

	var emails []Email

	result := DB.Model(&User{}).Find(&emails)
	if result.Error != nil {
		return nil, result.Error
	}

	emailList := []string{}
	for _, i := range emails {
		emailList = append(emailList, i.Email)
	}

	return emailList, nil
}

func FetchVerifiedBirthdayUsers(query FetchVerifiedBirthdayUsersQuery) ([]User, error) {
	var users []User

	querys := fmt.Sprintf("verified_status = %t", query.VerifiedStatus)

	if query.IsBirthday {
		// testDate := "1998-07-17"
		// testDate := "1998-06-22"

		// now, _ := time.Parse("2006-01-02", time.Now().String())
		// now, _ := time.Parse("2006-01-02", testDate)

		// query using birthday
		// querys = fmt.Sprintf("%s and birthday = '%s'", querys, now.Format("2006-01-02 15:04:05+00"))

		// query using minute joined - for scheduler test
		querys = fmt.Sprintf("%s and joined_minute_at = %d", querys, time.Now().Minute())
	}

	result := DB.Where(querys).Find(&users)
	if result.Error != nil {
		return nil, result.Error
	}
	return users, nil
}

func GeneratePromo(props CreatePromoFields) (string, error) {
	generator := crypto.MD5.New()
	fmt.Fprintf(generator, props.Name)
	fmt.Fprintf(generator, props.ValidUserId)
	fmt.Fprint(generator, props.StartDate.String())
	promoCode := hex.EncodeToString(generator.Sum(nil))

	result := DB.Create(&PromoCode{
		UserId:    uuid.MustParse(props.ValidUserId),
		Code:      promoCode,
		StartDate: props.StartDate,
		EndDate:   props.EndDate,
		Amount:    props.Amount,
	})
	if result.Error != nil {
		return "", result.Error
	}
	return promoCode, nil
}

func SendWhatsappNotification(params NotificationParams) error {
	var e error
	if params.NotificationType == "Whatsapp" {
		accountSid := os.Getenv("TWILIO_ACCOUNT_SID")
		authToken := os.Getenv("TWILIO_AUTH_TOKEN")
		sender := os.Getenv("TWILIO_WHATSAPP_SENDER")

		client := twilio.NewRestClientWithParams(twilio.ClientParams{
			Username: accountSid,
			Password: authToken,
		})

		msgBody := fmt.Sprintf("%s\n%s", params.Subject, params.Body)

		twParams := &twilioApi.CreateMessageParams{}
		twParams.SetTo(params.Target)
		twParams.SetFrom(sender)
		twParams.SetBody(msgBody)

		resp, err := client.Api.CreateMessage(twParams)
		if err != nil {
			fmt.Println("Twilio WA error : ", err)
			e = err
		} else {
			response, _ := json.Marshal(*resp)
			fmt.Println("Twilio success response : ", string(response))
			e = nil
		}
	} else {
		fmt.Println("Invalid Notification Type: ", params.NotificationType)
		e = fmt.Errorf("invalid Notification Type: %s", params.NotificationType)
	}

	return e
}

func ProcessPromo() {
	// fetch user
	users, err := FetchVerifiedBirthdayUsers(FetchVerifiedBirthdayUsersQuery{
		VerifiedStatus: true,
		IsBirthday:     true,
	})
	if err != nil {
		fmt.Println(err)
	}

	// generate promo
	for _, i := range users {
		nowDate := time.Now().Format("2006-01-02 00:00:00+00")
		startDate, err := time.Parse("2006-01-02 15:04:05+00", nowDate)
		if err != nil {
			fmt.Println(err)
		}

		endDate := startDate.Add(time.Hour * 24).Add(-time.Second * 1)
		promoCode, err := GeneratePromo(CreatePromoFields{
			Name:        i.Name,
			StartDate:   startDate,
			EndDate:     endDate,
			Amount:      10000.0,
			ValidUserId: i.ID.String(),
		})
		if err != nil {
			fmt.Println(err)
		}

		// send notif
		err = SendWhatsappNotification(NotificationParams{
			NotificationType: "Whatsapp",
			Subject:          fmt.Sprintf("Gift for %s's Birthday!", i.Name),
			Body:             fmt.Sprintf("Here's your promo code to use till EoD: %s", promoCode),
			Target:           fmt.Sprintf("whatsapp:%s",i.Phone),
		})
		if err != nil {
			fmt.Println(err)
		}
	}
}

func main() {
	InitDB()

	// emails, err := FetchUsers()
	// if err != nil {
	// 	fmt.Println(err)
	// }

	// query := FetchUserQuery{
	// 	Email: "ilham.arieshta@gmail.com",
	// 	VerifiedStatus: true,
	// 	IsBirthday: true,
	// }
	// user, err := FetchUser(query)
	// if err != nil {
	// 	panic(err)
	// }
	// fmt.Println("===", user)

	s := gocron.NewScheduler(time.Local)

	s.Every(1).Minute().Do(func() {
		ProcessPromo()
	})

	s.StartBlocking()
}

package messages

const (
	InvalidRequest    = "Invalid request format. Please ensure the structure is correct and matches the expected data format."
	MsgSomethingWrong = "Something went wrong"
	MsgInternal       = "Something went wrong. Please contact support with the log ID: %s."
	MsgCredential     = "Invalid Credentials. Please input the correct email or phone and password, then try again."
	MsgExists         = "Already exists."
	MsgNotFound       = "Data Not Found"
	NotFound          = "The requested resource could not be found"
	MsgSuccess        = "Success"
	InvalidCred       = "Invalid email, phone, or password"
	AccessDenied      = "Access denied. You do not have the required permissions."
)

const (
	ErrHashPassword = "crypto/bcrypt: hashedPassword is not the hash of the given password"
)

package validators

import (
	"log"
	"regexp"

	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
)

var accountNameRe = regexp.MustCompile(`^[\p{L}0-9 ]+$`)

func RegisterCustomValidators() {
	v, ok := binding.Validator.Engine().(*validator.Validate)
	if !ok {
		log.Fatal("Could not register custom validators")
	}

	v.RegisterValidation("accountname", func(fl validator.FieldLevel) bool {
		return accountNameRe.MatchString(fl.Field().String())
	})
}

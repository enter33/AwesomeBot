package context

import (
	"log"

	"github.com/tiktoken-go/tokenizer"

	"github.com/enter33/AwesomeBot/pkg/config"
)

var tokenEnc tokenizer.Codec

func init() {
	var err error
	tokenEnc, err = tokenizer.Get(tokenizer.Cl100kBase)
	if err != nil {
		log.Fatal(err)
	}
}

// CountTokens 计算消息的 token 数量
func CountTokens(message config.OpenAIMessage) int {
	contentAny := message.GetContent().AsAny()
	switch contentAny.(type) {
	case *string:
		count, _ := tokenEnc.Count(*contentAny.(*string))
		return count
	}
	return 0
}

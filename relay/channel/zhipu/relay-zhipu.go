package zhipu

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// https://open.bigmodel.cn/doc/api#chatglm_std
// chatglm_std, chatglm_lite
// https://open.bigmodel.cn/api/paas/v3/model-api/chatglm_std/invoke
// https://open.bigmodel.cn/api/paas/v3/model-api/chatglm_std/sse-invoke

var zhipuTokens sync.Map
var expSeconds int64 = 24 * 3600

func getZhipuToken(apikey string) string {
	data, ok := zhipuTokens.Load(apikey)
	if ok {
		tokenData := data.(zhipuTokenData)
		if time.Now().Before(tokenData.ExpiryTime) {
			return tokenData.Token
		}
	}

	split := strings.Split(apikey, ".")
	if len(split) != 2 {
		common.SysLog("invalid zhipu key: " + apikey)
		return ""
	}

	id := split[0]
	secret := split[1]

	expMillis := time.Now().Add(time.Duration(expSeconds)*time.Second).UnixNano() / 1e6
	expiryTime := time.Now().Add(time.Duration(expSeconds) * time.Second)

	timestamp := time.Now().UnixNano() / 1e6

	payload := jwt.MapClaims{
		"api_key":   id,
		"exp":       expMillis,
		"timestamp": timestamp,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, payload)

	token.Header["alg"] = "HS256"
	token.Header["sign_type"] = "SIGN"

	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		return ""
	}

	zhipuTokens.Store(apikey, zhipuTokenData{
		Token:      tokenString,
		ExpiryTime: expiryTime,
	})

	return tokenString
}

func requestOpenAI2Zhipu(request dto.GeneralOpenAIRequest) *ZhipuRequest {
	messages := make([]ZhipuMessage, 0, len(request.Messages))
	for _, message := range request.Messages {
		if message.Role == "system" {
			messages = append(messages, ZhipuMessage{
				Role:    "system",
				Content: message.StringContent(),
			})
			messages = append(messages, ZhipuMessage{
				Role:    "user",
				Content: "Okay",
			})
		} else {
			messages = append(messages, ZhipuMessage{
				Role:    message.Role,
				Content: message.StringContent(),
			})
		}
	}
	return &ZhipuRequest{
		Prompt:      messages,
		Temperature: request.Temperature,
		TopP:        lo.FromPtrOr(request.TopP, 0),
		Incremental: false,
	}
}

func responseZhipu2OpenAI(response *ZhipuResponse) *dto.OpenAITextResponse {
	fullTextResponse := dto.OpenAITextResponse{
		Id:      response.Data.TaskId,
		Object:  "chat.completion",
		Created: common.GetTimestamp(),
		Choices: make([]dto.OpenAITextResponseChoice, 0, len(response.Data.Choices)),
		Usage:   response.Data.Usage,
	}
	for i, choice := range response.Data.Choices {
		openaiChoice := dto.OpenAITextResponseChoice{
			Index: i,
			Message: dto.Message{
				Role:    choice.Role,
				Content: strings.Trim(choice.Content, "\""),
			},
			FinishReason: "",
		}
		if i == len(response.Data.Choices)-1 {
			openaiChoice.FinishReason = "stop"
		}
		fullTextResponse.Choices = append(fullTextResponse.Choices, openaiChoice)
	}
	return &fullTextResponse
}

func streamResponseZhipu2OpenAI(zhipuResponse string) *dto.ChatCompletionsStreamResponse {
	var choice dto.ChatCompletionsStreamResponseChoice
	choice.Delta.SetContentString(zhipuResponse)
	response := dto.ChatCompletionsStreamResponse{
		Object:  "chat.completion.chunk",
		Created: common.GetTimestamp(),
		Model:   "chatglm",
		Choices: []dto.ChatCompletionsStreamResponseChoice{choice},
	}
	return &response
}

func streamMetaResponseZhipu2OpenAI(zhipuResponse *ZhipuStreamMetaResponse) (*dto.ChatCompletionsStreamResponse, *dto.Usage) {
	var choice dto.ChatCompletionsStreamResponseChoice
	choice.Delta.SetContentString("")
	choice.FinishReason = &constant.FinishReasonStop
	response := dto.ChatCompletionsStreamResponse{
		Id:      zhipuResponse.RequestId,
		Object:  "chat.completion.chunk",
		Created: common.GetTimestamp(),
		Model:   "chatglm",
		Choices: []dto.ChatCompletionsStreamResponseChoice{choice},
	}
	return &response, &zhipuResponse.Usage
}

func zhipuStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	var usage *dto.Usage
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	scanner := helper.NewStreamScanner(resp.Body)
	scanner.Split(bufio.ScanLines)

	// The v3 SSE body interleaves plain `data:` text chunks with `meta:` frames,
	// and the meta frame carries the terminal usage. Both travel over one FIFO
	// channel so the consumer cannot reorder them: a separate completion signal
	// racing two buffered channels let [DONE] overtake queued chunks and usage.
	type zhipuStreamEvent struct {
		data string
		meta bool
	}
	// Buffered so the reader can hand off without waiting, bounded so a stalled
	// consumer cannot grow memory without limit.
	events := make(chan zhipuStreamEvent, 16)
	// Closed once the response is consumed so a reader parked on a full buffer
	// can still exit when the client disconnects.
	streamDone := make(chan struct{})
	defer close(streamDone)

	go func() {
		// Closing (rather than sending a completion event) is what orders
		// [DONE] after every frame that was read successfully.
		defer close(events)
		for scanner.Scan() {
			line := scanner.Text()
			var event zhipuStreamEvent
			switch {
			case strings.HasPrefix(line, "data:"):
				event = zhipuStreamEvent{data: strings.TrimPrefix(line, "data:")}
			case strings.HasPrefix(line, "meta:"):
				event = zhipuStreamEvent{data: strings.TrimPrefix(line, "meta:"), meta: true}
			default:
				continue
			}
			select {
			case events <- event:
			case <-ctx.Done():
				return
			case <-streamDone:
				return
			}
		}
		if err := scanner.Err(); err != nil {
			common.SysLog("error reading stream: " + err.Error())
		}
	}()

	helper.SetEventStreamHeaders(c)
	c.Stream(func(w io.Writer) bool {
		select {
		case <-ctx.Done():
			return false
		case event, ok := <-events:
			if !ok {
				// The reader closes the channel only after draining the body, so
				// every chunk and the usage frame have already been rendered. A
				// closed channel caused by cancellation must not look like a
				// normally completed stream.
				if ctx.Err() != nil {
					return false
				}
				c.Render(-1, common.CustomEvent{Data: "data: [DONE]"})
				return false
			}
			if event.meta {
				var zhipuResponse ZhipuStreamMetaResponse
				if err := common.UnmarshalJsonStr(event.data, &zhipuResponse); err != nil {
					common.SysLog("error unmarshalling stream response: " + err.Error())
					return true
				}
				response, zhipuUsage := streamMetaResponseZhipu2OpenAI(&zhipuResponse)
				jsonResponse, err := common.Marshal(response)
				if err != nil {
					common.SysLog("error marshalling stream response: " + err.Error())
					return true
				}
				usage = zhipuUsage
				c.Render(-1, common.CustomEvent{Data: "data: " + string(jsonResponse)})
				return true
			}
			response := streamResponseZhipu2OpenAI(event.data)
			jsonResponse, err := common.Marshal(response)
			if err != nil {
				common.SysLog("error marshalling stream response: " + err.Error())
				return true
			}
			c.Render(-1, common.CustomEvent{Data: "data: " + string(jsonResponse)})
			return true
		}
	})
	service.CloseResponseBodyGracefully(resp)
	return usage, nil
}

func zhipuHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	var zhipuResponse ZhipuResponse
	responseBody, err := common.ReadUpstreamBodyForRequest(c, resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	service.CloseResponseBodyGracefully(resp)
	err = common.Unmarshal(responseBody, &zhipuResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if !zhipuResponse.Success {
		return nil, types.WithOpenAIError(types.OpenAIError{
			Message: zhipuResponse.Msg,
			Code:    zhipuResponse.Code,
		}, resp.StatusCode)
	}
	fullTextResponse := responseZhipu2OpenAI(&zhipuResponse)
	jsonResponse, err := common.Marshal(fullTextResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	_, err = c.Writer.Write(jsonResponse)
	return &fullTextResponse.Usage, nil
}

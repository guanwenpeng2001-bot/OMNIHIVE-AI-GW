package repository

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

// Keep the existing JSON representation and TTL for codes issued before an
// upgrade. Checking, charging the attempt and consuming success share one script
// so a concurrent resend cannot have its new code deleted by an old request.
var verifyEmailCodeScript = redis.NewScript(`
local raw = redis.call('GET', KEYS[1])
if not raw or redis.call('PTTL', KEYS[1]) <= 0 then return -2 end
local data = cjson.decode(raw)
if type(data.Code) ~= 'string' or type(data.Attempts) ~= 'number' then return -2 end
local cap = tonumber(ARGV[2])
if not cap or cap <= 0 or data.Attempts < 0 then return -2 end
if data.Attempts >= cap then return -1 end
data.Attempts = data.Attempts + 1
local matches = #data.Code == #ARGV[1] and #data.Code > 0
if matches then
    local difference = 0
    for i = 1, #data.Code do
        difference = difference + math.abs(string.byte(data.Code, i) - string.byte(ARGV[1], i))
    end
    matches = difference == 0
end
if matches then
    redis.call('DEL', KEYS[1])
    return 1
end
redis.call('SET', KEYS[1], cjson.encode(data), 'KEEPTTL')
if data.Attempts >= cap then return -1 end
return 0
`)

func (c *emailCache) VerifyVerificationCode(ctx context.Context, email, code string, maxAttempts int) error {
	return c.verifyEmailCode(ctx, verifyCodeKey(email), code, maxAttempts)
}

func (c *emailCache) VerifyNotifyCode(ctx context.Context, email, code string, maxAttempts int) error {
	return c.verifyEmailCode(ctx, notifyVerifyKey(email), code, maxAttempts)
}

func (c *emailCache) verifyEmailCode(ctx context.Context, key, code string, maxAttempts int) error {
	result, err := verifyEmailCodeScript.Run(ctx, c.rdb, []string{key}, code, maxAttempts).Int()
	if err != nil {
		return err
	}
	switch result {
	case 1:
		return nil
	case -1:
		return service.ErrVerifyCodeMaxAttempts
	default:
		return service.ErrInvalidVerifyCode
	}
}

var consumePasswordResetScript = redis.NewScript(`
local raw = redis.call('GET', KEYS[1])
if not raw or redis.call('PTTL', KEYS[1]) <= 0 then return 0 end
local data = cjson.decode(raw)
if data.Token ~= ARGV[1] then return 0 end
redis.call('DEL', KEYS[1])
return 1
`)

func (c *emailCache) ConsumePasswordResetToken(ctx context.Context, email, expectedToken string) (bool, error) {
	result, err := consumePasswordResetScript.Run(ctx, c.rdb, []string{passwordResetKey(email)}, expectedToken).Int()
	return result == 1, err
}

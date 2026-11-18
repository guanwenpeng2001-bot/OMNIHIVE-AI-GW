package repository

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"github.com/redis/go-redis/v9"
)

const imageTaskKeyPrefix = "image_task:"

type imageTaskStore struct {
	rdb *redis.Client
}

func NewImageTaskStore(rdb *redis.Client) service.ImageTaskStore {
	return &imageTaskStore{rdb: rdb}
}

func (s *imageTaskStore) Save(ctx context.Context, task *service.ImageTaskRecord, ttl time.Duration) error {
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	result, err := imageTaskSaveScript.Run(ctx, s.rdb, []string{imageTaskKey(task.ID), "image_task:active"}, string(data), ttl.Milliseconds(), time.Now().Unix(), task.ID, task.ExecutionDeadline).Int()
	if err != nil {
		return err
	}
	if result < 0 {
		return service.ErrImageTaskUnavailable
	}
	return nil
}

func (s *imageTaskStore) Get(ctx context.Context, id string) (*service.ImageTaskRecord, error) {
	data, err := s.rdb.Get(ctx, imageTaskKey(id)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, service.ErrImageTaskNotFound
		}
		return nil, err
	}
	var task service.ImageTaskRecord
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func imageTaskKey(id string) string {
	return imageTaskKeyPrefix + strings.TrimSpace(id)
}

// Creation/admission and terminal writes are atomic across instances. The record
// is the recovery receipt; only bounded execution metadata is kept in Redis.
var imageTaskSaveScript = redis.NewScript(`
local incoming = cjson.decode(ARGV[1])
local previous = redis.call('GET', KEYS[1])
if previous then
 local current = cjson.decode(previous)
 if current.status ~= 'processing' then return 0 end
 if (incoming.version or 0) ~= (current.version or 0) + 1 then return 0 end
else
 if incoming.status ~= 'processing' then return 0 end
 redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', ARGV[3])
 if redis.call('ZCARD', KEYS[2]) >= 1024 then return -1 end
 local deadline = tonumber(ARGV[5])
 if deadline == 0 then deadline = tonumber(ARGV[3]) + 1800 end
 redis.call('ZADD', KEYS[2], deadline, ARGV[4])
end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
if incoming.status ~= 'processing' then redis.call('ZREM', KEYS[2], ARGV[4]) end
return 1
`)

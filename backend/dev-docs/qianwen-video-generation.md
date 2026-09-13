# Qianwen asynchronous video requests and billing

The native endpoint is `POST /api/v1/services/aigc/video-generation/video-synthesis`.
Use a gateway API key assigned to a Qianwen group with explicit `video_model_prices`.
The gateway forwards `X-DashScope-Async: enable` to DashScope and returns HTTP 202
with a public task ID. Query `GET /api/v1/tasks/{public_task_id}` using the same key.

## Official request shapes

Wan text-to-video uses `parameters.size`; `parameters.resolution` is not its
resolution selector. Wan image-to-video uses `parameters.resolution` and requires
`input.img_url`. A prompt belongs inside `input`, and duration belongs inside
`parameters`. These examples explicitly request five seconds, which is supported
by both models. The gateway requires an explicit resolution for its price snapshot,
even where the upstream API permits omitting it.

[Official Wan 2.1–2.6 text-to-video reference](https://help.aliyun.com/en/model-studio/legacy-wan-text-to-video-api-reference)

```json
{
  "model": "wan2.6-t2v",
  "input": {
    "prompt": "A small orange cat walks slowly through a sunlit garden, static camera, natural colors."
  },
  "parameters": {
    "size": "1280*720",
    "duration": 5,
    "prompt_extend": false,
    "seed": 260914
  }
}
```

[Official Wan 2.1–2.6 image-to-video reference](https://help.aliyun.com/en/model-studio/legacy-image-to-video-api-reference/)

```json
{
  "model": "wan2.6-i2v",
  "input": {
    "prompt": "The cat walks slowly on the grass, a steady camera follows the gentle motion.",
    "img_url": "https://cdn.translate.alibaba.com/r/wanx-demo-1.png"
  },
  "parameters": {
    "resolution": "1080P",
    "duration": 5,
    "prompt_extend": false,
    "seed": 260914
  }
}
```

The admission check rejects the wrong resolution field before creating or sending
a job. It also recognizes official square and 4:3 text-to-video sizes, such as
`960*960` (720p) and `1632*1248` (1080p).

## Completion and settlement

The client GET reads durable state; it does not trigger upstream polling. The
server schedules a first poll after five seconds, then delays of 5, 10, 20, 40,
80, 160 and at most 320 seconds. Timing-wheel scan cadence and network latency
can make observed intervals longer. Temporary network/429/5xx failures are retried;
ambiguous submissions are not automatically submitted again.

For Wan 2.6, billing uses authoritative `usage.duration`, `video_count=1` and
resolution evidence (`SR` and, when present, `size`). The result must include a
video URL and match the admission-time tier. Cost is duration multiplied by the
stored per-second price; the stored group/user multiplier determines actual cost.
Uninterpretable usage or a mismatched tier remains uncharged.

Settlement locks the job row and inserts the usage log, applies billing effects,
and updates `charged_usage_log_id` in one transaction. A terminal-state/anchor
check prevents repeated settlement. Equivalent JSON submissions retain the same
public job ID, including after completion; different key order and whitespace
are canonicalized. A fresh generation therefore needs a deliberately different
request, for example another seed.

## Live verification: 2026-09-14, existing :8080

The examples above completed against the existing process without a restart:

| Model | Tier / duration | Public task ID | Job / usage ID | Server elapsed | Total and actual cost |
| --- | --- | --- | --- | --- | --- |
| wan2.6-t2v | 720p / 5s | ba4a9a48-ff50-4163-ae98-a6da40cc4a47 | 2 / 125 | 58.544954s | 0.42857143 |
| wan2.6-i2v | 1080p / 5s | ea9a2a74-d129-40ad-9fe9-48eabee25432 | 3 / 126 | 52.920910s | 0.71428571 |

The group 13 rates were respectively 0.08571428571428572 and
0.14285714285714285 per second, with multiplier 1. Twenty concurrent terminal
GETs (ten per job) and two canonically equivalent POST replays left each job
with exactly one usage row, unchanged cost and unchanged settlement anchor.

The prior job 1 retained `unsupported_usage` and zero usage rows: its snapshot
was 720p but the upstream returned `SR=1080`, `size=1920*1080`, duration 5.
Correcting the t2v request resolved live settlement without loosening this check.

The new admission guard and aspect-ratio mappings are covered by unit tests;
they were not loaded into the already-running process during this verification.
The i2v intermediate RUNNING state was not sampled; only PENDING and SUCCEEDED
were observed for that job. No claim is made about its exact RUNNING transition.

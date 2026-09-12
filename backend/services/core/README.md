# core

Everything boring about the people using Surge, as one service until it hurts.
It starts with profiles: the first name and the photo a rider and a driver see
of each other. It scales on nothing in particular, which is why the next
boring thing — vehicles, say — belongs here too rather than in a service of its
own.

## Photos

```
app ── crop to a square, re-encode ──▶ POST /v1/profile/avatar ──▶ gateway ──gRPC──▶ core
                                                                                     │ decode (JPEG, PNG, WebP; ≤ 5 MB, ≤ 8000 px a side)
                                                                                     │ middle square, ≤ 512 px, JPEG — pixels only
                                                                                     │ PUT avatars/<user>/<hash>.jpg to R2
                                                                                     │ record the key, delete the one it replaced
anyone signed in ◀── avatar_url: a link presigned for 2 h, signed as of the hour ──┘
```

- **Through the API, not straight to the bucket.** The photo is re-encoded
  from its pixels before anything is stored, so what a phone writes into a
  photo — the place it was taken included, which for a selfie is usually
  someone's home — is never kept, not even briefly. It also leaves the bucket
  private with no CORS to configure.
- **Content-addressed keys.** A new photo is a new key, so a cached link can
  never show the wrong face, and the object behind a key never changes.
- **Links that stay put for an hour.** Signed as of the start of the hour and
  good for two, every caller within the hour gets the same URL, which a browser
  caches, and it still works an hour later.
- **Stored, then recorded.** A photo recorded but not stored would be a broken
  image on everyone's screen; stored but not recorded is only an object nothing
  links to.

## Who sees what

Any signed-in caller may read any profile by user id. An id reaches somebody
only through a trip or a conversation they share, and a first name and a photo
are what that person sees of them anyway. Nothing else is in a profile.

## Configuration

| variable               | default      |                                                     |
| ---------------------- | ------------ | --------------------------------------------------- |
| `DATABASE_URL`         | required     |                                                     |
| `AVATAR_STORE`         | required     | `r2`, or `memory` for a laptop without R2's keys    |
| `R2_ACCOUNT_ID`        | with r2      | the id in the bucket's S3 URL                       |
| `R2_BUCKET`            | `surge`      |                                                     |
| `R2_ACCESS_KEY_ID`     | with r2      | an R2 API token scoped to this bucket, read + write |
| `R2_SECRET_ACCESS_KEY` | with r2      |                                                     |
| `R2_ENDPOINT`          | from account | `https://<account>.r2.cloudflarestorage.com`        |
| `CORE_GRPC_LISTEN`     | `:8114`      |                                                     |
| `CORE_METRICS_ADDR`    | `:9109`      |                                                     |

`make dev-core` runs it with `AVATAR_STORE=memory` unless `.env` chooses r2.

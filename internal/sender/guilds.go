package sender

import "context"

const RouteGuild = "/api/ingest/guild-roster"

func (s *Sender) PostGuild(ctx context.Context, token string, body []byte, ids []string) Resultat {
	return s.envoie(ctx, token, body, RouteGuild, "forever-pulse-guild-roster", "1", ids)
}

# ovh-sms-messaging

Envoie des SMS via l'API [http2sms d'OVHcloud](https://help.ovhcloud.com/csm/en-gb-sms-sending-via-url-http2sms)
à partir de messages déposés dans une queue [Scaleway Queues](https://www.scaleway.com/en/docs/queues/).

L'application tourne dans un **Serverless Container** Scaleway. Un **trigger** lit la queue
et pousse chaque message en `POST` HTTP vers le conteneur : pas de polling, pas de SDK,
uniquement la bibliothèque standard Go.

```
Producteur ──▶ Scaleway Queue ──Trigger──▶ POST / ──▶ Serverless Container ──▶ OVH http2sms
                    │
                    └──▶ DLQ
```

## Format du message

```json
{
  "to": ["+33612345678", "+33700000000"],
  "message": "Votre code est 123456",
  "sender": "MYAPP",
  "tag": "otp"
}
```

| Champ | Obligatoire | Description |
|---|---|---|
| `to` | oui | Destinataires au format international (`+33…` ou `0033…`, espaces, points et tirets tolérés). Les numéros nationaux (`06…`) sont refusés. |
| `message` | oui | Texte du SMS. |
| `sender` | non | Expéditeur déclaré sur le compte SMS. Par défaut : `OVH_SMS_SENDER`. |
| `tag` | non | Marqueur OVH, 20 caractères max. |

## Réponses au trigger

Le trigger supprime le message sur une réponse 2xx et le réessaie (3 fois max) sinon.

| Cas | Réponse | Effet |
|---|---|---|
| SMS envoyé (statut OVH 100–199) | `200` | Message supprimé |
| Erreur temporaire : réseau, timeout, HTTP ≠ 200, statut OVH 401 (IP non autorisée)… | `503` | Réessayé, puis DLQ |
| Erreur définitive : JSON invalide, numéro invalide, statut OVH 201/202 | `200` | Message supprimé, erreur loguée |

Les erreurs définitives répondent `200` exprès : les réessayer échouerait de la même façon.

## Configuration

| Variable | Obligatoire | Défaut | Description |
|---|---|---|---|
| `OVH_SMS_ACCOUNT` | oui | | Compte SMS, ex. `sms-xx11111-1` |
| `OVH_SMS_LOGIN` | oui | | Utilisateur SMS (créé dans l'espace client OVH, ce n'est pas le NIC) |
| `OVH_SMS_PASSWORD` | oui | | Mot de passe de l'utilisateur SMS — **à déclarer en secret** |
| `OVH_SMS_SENDER` | oui | | Expéditeur par défaut |
| `OVH_SMS_NO_STOP` | non | `false` | `true` pour retirer la mention STOP (SMS non commerciaux) |
| `OVH_TIMEOUT` | non | `10s` | Timeout de l'appel OVH |
| `OVH_SMS_ENDPOINT` | non | `https://www.ovh.com/cgi-bin/sms/http2sms.cgi` | Utile pour les tests |
| `LOG_LEVEL` | non | `info` | `debug`, `info`, `warn`, `error` |
| `PORT` | non | `8080` | Fourni par Scaleway |

Les logs sont en JSON sur la sortie standard (visibles dans Cockpit). Les numéros y sont
masqués et le mot de passe n'y apparaît jamais. En `debug`, les headers envoyés par le
trigger sont logués (hors credentials).

## Développement

```sh
go test ./...

OVH_SMS_ACCOUNT=sms-xx11111-1 OVH_SMS_LOGIN=user OVH_SMS_PASSWORD=secret OVH_SMS_SENDER=MYAPP \
  go run ./cmd/ovh-sms-messaging

curl -i -X POST localhost:8080/ -d '{"to":["+33612345678"],"message":"test"}'
curl -i localhost:8080/healthz
```

## Déploiement sur Scaleway

1. **Image** — Scaleway Serverless Containers exige une image `linux/amd64` :
   ```sh
   docker build --platform linux/amd64 -t rg.fr-par.scw.cloud/<namespace>/ovh-sms-messaging:<version> .
   docker push rg.fr-par.scw.cloud/<namespace>/ovh-sms-messaging:<version>
   ```
2. **Queues** — créer une queue Standard et sa DLQ (même projet, même région) :
   - DLQ reliée par une redrive policy, `maxReceiveCount` = 4 ;
   - **durée de rétention** réglée explicitement, suffisante pour absorber les pics et les cold starts ;
   - **visibility timeout** supérieur au temps de traitement (30 s ou plus avec `OVH_TIMEOUT=10s`).
3. **Conteneur** :
   - privacy **privée** — un conteneur public permettrait à n'importe qui d'envoyer des SMS ;
   - `min_scale = 0`, `max_scale` bas (1 ou 2) pour ne pas dépasser le débit accepté par OVH ;
   - variables ci-dessus, avec `OVH_SMS_PASSWORD` en *secret environment variable*.
4. **Trigger** — onglet *Triggers* du conteneur, type Scaleway Queues, sur la queue principale.
5. **OVH** — restreindre l'utilisateur SMS aux IP sortantes utilisées, si possible.

Au premier déploiement, vérifier que le trigger atteint bien le conteneur privé, et
passer `LOG_LEVEL=debug` le temps de voir les headers qu'il envoie.

## Limites connues

- **SMS en double possibles** : la livraison est « au moins une fois ». Si OVH envoie le SMS
  mais que la réponse se perd (timeout, arrêt du conteneur), le trigger rejoue le message.
- http2sms transmet le mot de passe dans l'URL : c'est imposé par l'API OVH.

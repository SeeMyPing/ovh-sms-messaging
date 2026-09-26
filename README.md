# sqs-to-smpp-gateway

Envoie en SMS les messages déposés dans une queue, au choix :

- en **SMPP 3.4**, vers n'importe quel SMSC ;
- via l'**API HTTP** d'un fournisseur : [Twilio](https://www.twilio.com/docs/messaging/api/message-resource),
  [OVHcloud](https://docs.ovhcloud.com/en/guides/web-cloud/messaging/sms/send-sms-http2sms) (http2sms)
  ou [ClickSend](https://developers.clicksend.com/docs/rest/v3/).

Le protocole et le fournisseur se choisissent par variable d'environnement ou par option
(voir [Choix du fournisseur](#choix-du-fournisseur)).

Elle reçoit les messages de la queue en `POST` HTTP. Le déploiement fourni utilise
[Scaleway Queues](https://www.scaleway.com/en/docs/queues/) et un **trigger** qui pousse
chaque message vers un **Serverless Container** (voir [`deploy/`](deploy)).

```
Producteur ──▶ Queue ──Trigger──▶ POST / ──▶ Conteneur ──SMPP──▶ SMSC
                 │                                   └──HTTPS─▶ Twilio / OVH / ClickSend
                 └──▶ DLQ
```

## Format du message

```json
{
  "to": "+33612345678",
  "message": "Votre code est 123456",
  "sender": "MYAPP"
}
```

| Champ | Obligatoire | Description |
|---|---|---|
| `to` | oui | **Un seul** destinataire, au format international (`+33…` ou `0033…`, espaces, points et tirets tolérés). Les numéros nationaux (`06…`) sont refusés. |
| `message` | oui | Texte du SMS, 1600 caractères max. |
| `sender` | non | Expéditeur : alphanumérique (11 caractères ASCII max), numéro court ou numéro international `+…`. Par défaut : `SMS_SENDER`. Le fournisseur peut n'accepter que certains expéditeurs. |

Pour envoyer à plusieurs destinataires, déposer un message par destinataire.

### Encodage et messages longs

En SMPP, l'application encode et découpe elle-même le message :

- **GSM 03.38** (`data_coding` 0) si tous les caractères en font partie : 160 caractères par SMS.
- **UCS-2** (`data_coding` 8) sinon (accents hors GSM, emojis, alphabets non latins) : 70 caractères par SMS.
- Au-delà, le message part en plusieurs SMS concaténés (UDH, `esm_class` 0x40) :
  153 caractères GSM ou 67 UCS-2 par partie. Un caractère n'est jamais coupé entre deux parties.

Via une API HTTP, le texte est transmis tel quel et le fournisseur découpe (pour OVH,
`smsCoding` vaut 1 si le texte tient en GSM 03.38, 2 sinon).

## Réponses au trigger

Le trigger supprime le message sur une réponse 2xx et le réessaie (3 fois max) sinon.

| Cas | Réponse | Effet |
|---|---|---|
| SMS accepté par le SMSC ou l'API | `200` | Message supprimé |
| Message invalide : JSON, numéro, longueur, expéditeur | `200` | Message supprimé, erreur loguée |
| Refusé par le fournisseur pour le message lui-même (voir ci-dessous) | `200` | Message supprimé, erreur loguée |
| Tout le reste : réseau, timeout, identifiants, IP non autorisée, limitation de débit, erreur serveur, crédit épuisé, expéditeur refusé… | `503` | Réessayé, puis DLQ |

Erreurs traitées comme définitives, par fournisseur :

| Fournisseur | Erreurs définitives |
|---|---|
| SMPP | `ESME_RINVDSTADR`, `ESME_RINVDSTTON`, `ESME_RINVDSTNPI`, `ESME_RINVMSGLEN` |
| Twilio | `21211` (numéro invalide), `21602` (texte vide), `21610` (destinataire désabonné), `21614` (pas un mobile), `21617` (texte trop long) |
| OVH | statuts `201` (paramètre manquant), `202` (paramètre invalide) |
| ClickSend | statuts de message `INVALID_RECIPIENT`, `EMPTY_MESSAGE` |

Les erreurs définitives répondent `200` exprès : les réessayer échouerait de la même façon.
Les erreurs qui dépendent de la configuration (identifiants, IP autorisées, expéditeur)
sont traitées comme temporaires : les messages restent récupérables dans la DLQ.

« Accepté par le SMSC » (ou par l'API) ne veut pas dire « livré » : les accusés de
réception (DLR) ne sont pas gérés.

## Choix du fournisseur

| Variable | Option | Valeurs | Défaut |
|---|---|---|---|
| `SMS_PROTOCOL` | `-protocol` | `smpp`, `http` | `smpp` |
| `SMS_PROVIDER` | `-provider` | `twilio`, `ovh`, `clicksend` | |

L'option l'emporte sur la variable d'environnement. `SMS_PROVIDER` est obligatoire en
`http`. En `smpp`, n'importe quel SMSC convient (y compris ceux de ces fournisseurs) :
`SMS_PROVIDER` est alors facultatif, libre, et n'apparaît que dans les logs.

```sh
sqs-to-smpp-gateway -protocol http -provider twilio
sqs-to-smpp-gateway -h
```

Seules les variables du protocole et du fournisseur choisis sont lues et vérifiées.

### Ajouter un fournisseur

Chaque fournisseur est un package de [`internal/`](internal) exposant un `Config`, un
`NewClient` et un client qui implémente `Send(ctx, message.SMS) ([]string, error)` et
`Close() error`. Ses erreurs définitives implémentent `Permanent() bool`. Il reste à lire
sa configuration dans [`internal/config`](internal/config/config.go) et à l'ajouter à
`newProvider` dans [`cmd/sqs-to-smpp-gateway/main.go`](cmd/sqs-to-smpp-gateway/main.go).

## Session SMPP

- Bind **transmitter** au premier message, puis session conservée tant que l'instance vit
  et partagée entre les requêtes concurrentes.
- `enquire_link` périodique ; rebind automatique si la connexion tombe.
- `unbind` à l'arrêt (SIGTERM), après la fin des envois en cours.
- Chaque instance ouvre sa propre session : le nombre d'instances ne doit pas dépasser
  le nombre de binds autorisés par le fournisseur.

## Configuration

Communes :

| Variable | Obligatoire | Défaut | Description |
|---|---|---|---|
| `SMS_PROTOCOL` | non | `smpp` | Voir [Choix du fournisseur](#choix-du-fournisseur) |
| `SMS_PROVIDER` | en `http` | | Voir [Choix du fournisseur](#choix-du-fournisseur) |
| `SMS_SENDER` | selon le fournisseur | | Expéditeur par défaut (mêmes règles que `sender`) |
| `LOG_LEVEL` | non | `info` | `debug`, `info`, `warn`, `error` |
| `PORT` | non | `8080` | Port HTTP |

SMPP (`SMS_PROTOCOL=smpp`, `SMS_SENDER` obligatoire) :

| Variable | Obligatoire | Défaut | Description |
|---|---|---|---|
| `SMPP_ADDR` | oui | | Adresse du SMSC, `hôte:port` |
| `SMPP_SYSTEM_ID` | oui | | Identifiant SMPP |
| `SMPP_PASSWORD` | oui | | Mot de passe SMPP — **à déclarer en secret** |
| `SMPP_SYSTEM_TYPE` | non | vide | `system_type`, si le fournisseur en demande un |
| `SMPP_TLS` | non | `false` | Connexion TLS au SMSC |
| `SMPP_CONNECT_TIMEOUT` | non | `10s` | Connexion TCP et bind |
| `SMPP_SUBMIT_TIMEOUT` | non | `10s` | Attente de chaque `submit_sm_resp` |
| `SMPP_ENQUIRE_LINK` | non | `30s` | Période du keep-alive |

API HTTP (`SMS_PROTOCOL=http`) :

| Variable | Obligatoire | Défaut | Description |
|---|---|---|---|
| `SMS_API_TIMEOUT` | non | `10s` | Timeout de chaque appel à l'API |
| **Twilio** | | | `SMS_SENDER` ou `TWILIO_MESSAGING_SERVICE_SID` obligatoire |
| `TWILIO_ACCOUNT_SID` | oui | | Account SID (`AC…`) |
| `TWILIO_AUTH_TOKEN` | oui | | Auth Token — **à déclarer en secret** |
| `TWILIO_MESSAGING_SERVICE_SID` | non | | Messaging Service (`MG…`) : choisit l'expéditeur quand aucun n'est donné |
| **OVH** (http2sms) | | | `SMS_SENDER` obligatoire, déclaré sur le compte SMS |
| `OVH_SMS_ACCOUNT` | oui | | Compte SMS, ex. `sms-xx11111-1` |
| `OVH_SMS_LOGIN` | oui | | Utilisateur SMS (créé dans l'espace client OVH, ce n'est pas le NIC) |
| `OVH_SMS_PASSWORD` | oui | | Mot de passe de l'utilisateur SMS — **à déclarer en secret** |
| `OVH_SMS_NO_STOP` | non | `false` | `true` pour retirer la mention STOP (SMS non commerciaux) |
| **ClickSend** | | | `SMS_SENDER` facultatif : sans lui, envoi depuis un numéro partagé |
| `CLICKSEND_USERNAME` | oui | | Nom d'utilisateur API |
| `CLICKSEND_API_KEY` | oui | | Clé API — **à déclarer en secret** |

Les logs sont en JSON sur la sortie standard. Les numéros y sont masqués et les mots de
passe et clés n'y apparaissent jamais. En `debug`, les headers HTTP reçus sont logués (hors credentials).

## Développement

Avec le simulateur [smscsim](https://github.com/ukarim/smscsim) :

```sh
go test ./...

docker run -d -p 2775:2775 -p 12775:12775 ukarim/smscsim

SMPP_ADDR=localhost:2775 SMPP_SYSTEM_ID=test SMPP_PASSWORD=test SMS_SENDER=MYAPP \
  go run ./cmd/sqs-to-smpp-gateway

# ou via une API HTTP, par exemple Twilio
TWILIO_ACCOUNT_SID=AC… TWILIO_AUTH_TOKEN=… SMS_SENDER=+33700000000 \
  go run ./cmd/sqs-to-smpp-gateway -protocol http -provider twilio

curl -i -X POST localhost:8080/ -d '{"to":"+33612345678","message":"test"}'
curl -i localhost:8080/healthz
```

Les tests du client SMPP tournent contre un faux SMSC embarqué
([`internal/smpp/fake_smsc_test.go`](internal/smpp/fake_smsc_test.go)), ceux des clients
HTTP contre un serveur `httptest`.

## Déploiement

| Cible | Dossier |
|---|---|
| Scaleway (Queues + Serverless Containers) | [`deploy/scaleway`](deploy/scaleway) |

L'image est publiée par la [CI](#ci) sur `ghcr.io/seemyping/sqs-to-smpp-gateway`
(`linux/amd64`).

**IP sortante** : beaucoup de fournisseurs SMPP (et OVH, si l'utilisateur SMS est restreint
par IP) n'acceptent que des IP déclarées. Une plateforme serverless n'a en général pas
d'IP de sortie fixe : dans ce cas l'envoi échoue, les messages sont réessayés puis
conservés en DLQ. Twilio et ClickSend n'imposent pas de restriction d'IP par défaut.

## CI

Le workflow [`.github/workflows/ci.yml`](.github/workflows/ci.yml) lance trois jobs :

| Job | Contenu |
|---|---|
| `Go` | `gofmt`, `go vet`, `go test -race` |
| `Terraform` | `terraform fmt -check`, `terraform validate` sur `deploy/scaleway` |
| `Image` | Build de l'image `linux/amd64` ; publiée sur ghcr.io hors pull requests |

Tags publiés :

| Événement | Tags |
|---|---|
| Pull request | aucun (build seul) |
| Merge sur `main` | `sha-<commit>`, `latest` |
| Tag `vX.Y.Z` | `X.Y.Z`, `sha-<commit>` |

### Protection de `main`

[`.github/rulesets/main.json`](.github/rulesets/main.json) définit un ruleset GitHub :
pull request obligatoire (sans approbation requise, conversations résolues), jobs `Go`,
`Terraform` et `Image` verts sur une branche à jour, ni force-push ni suppression.

Les rulesets ne sont pas appliqués depuis le dépôt : il faut les importer une fois.

- Interface : *Settings → Rules → Rulesets → New ruleset → Import a ruleset*, choisir le fichier.
- CLI :
  ```sh
  gh api -X POST repos/SeeMyPing/sqs-to-smpp-gateway/rulesets --input .github/rulesets/main.json
  ```

Après une modification du fichier, mettre à jour le ruleset existant
(`gh api -X PUT repos/SeeMyPing/sqs-to-smpp-gateway/rulesets/<id> --input …`).

## Limites connues

- **SMS en double possibles** : la livraison est « au moins une fois ». Si le SMSC accepte
  le SMS mais que la réponse se perd (timeout, arrêt du conteneur), le trigger rejoue le
  message. En SMPP, pour un message long, un échec sur une partie fait renvoyer toutes
  les parties.
- **Pas de DLR** : les accusés de réception ne sont pas remontés.

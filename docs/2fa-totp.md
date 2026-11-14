# Dvofaktorska autentifikacija (TOTP)

## Cilj

Korisnik može uključiti dvofaktorsku autentifikaciju putem authenticator aplikacije
(Google Authenticator, Aegis, 1Password, ...). Kada je 2FA uključen, prijava traži
lozinku **i** 6-znamenkasti kod. Recovery kodovi služe ako korisnik izgubi uređaj.

## Odluke

- **TOTP po RFC 6238**: SHA1, 6 znamenki, period 30 s, tolerancija ±1 korak.
- **Vlastita implementacija TOTP-a** (stdlib: `crypto/hmac`, `crypto/sha1`,
  `encoding/base32`), bez `pquerna/otp`.
- **QR kod**: `github.com/skip2/go-qrcode`, PNG ugrađen u template kao `data:` URI.
- **Šifriranje secreta**: secret se u bazi čuva šifriran AES-GCM-om. Ključ dolazi
  iz ini postavke `totpkey` (32 bajta, base64) ili istoimene env varijable.
- **Recovery kodovi**:
  - 10 kodova po korisniku;
  - svaki kod ima 10 znakova iz Crockford base32 alfabeta (bez `I L O U`), što daje
    50 bita entropije;
  - prikaz u formatu `XXXXX-XXXXX`; pri unosu se pretvaraju u velika slova i uklanjaju
    se crtice i razmaci.
  - Spremaju se kao **bcrypt hash** (`util.HashPassword`). Prema NIST SP 800-63B,
    lookup tajne ispod 112 bita moraju se čuvati salted password hashingom; brzi hash
    poput SHA-256 nije dovoljan.
  - Potrošnja je ipak atomska: bcrypt pronađe odgovarajući kod, a zatim uvjetni
    `UPDATE` po **ID-u koda** potvrdi da kod još nije iskorišten (vidi 3.2).
  - Provjera prolazi kroz do 10 bcrypt usporedbi (oko 0,5–1 s). To je prihvatljivo
    jer je ruta ograničena rate limitom, a usporedbe se rade **prije** transakcije,
    tako da ne drže zaključane redove.
- **Stanje drugog koraka čuva se na serveru**, u redu tablice `sessions`. Cookie
  samo identificira sesiju.

## Opseg

### 1. Baza

1. Nova migracija `migrations/sql/030_two_factor.sql`.
2. U `migrations/migrations.go` postaviti `SchemaVersion = 30` i dodati
   `30: "sql/030_two_factor.sql"` u `migrationPaths`. Bez toga se migracija neće
   pokrenuti.
3. Iste promjene unijeti u `db/pg_structure.sql`.

```sql
ALTER TABLE users
  ADD COLUMN totp_secret     text,
  ADD COLUMN totp_enabled_at timestamptz,
  ADD COLUMN totp_last_step  bigint NOT NULL DEFAULT 0;

ALTER TABLE sessions
  ADD COLUMN pending_2fa_user_id     integer,
  ADD COLUMN pending_2fa_at          timestamptz,
  ADD COLUMN pending_2fa_failures    integer NOT NULL DEFAULT 0,
  ADD COLUMN pending_totp_secret     text,         -- šifrirani setup secret
  ADD COLUMN pending_totp_created_at timestamptz;

CREATE INDEX sessions_pending_2fa_user_idx
  ON sessions (pending_2fa_user_id) WHERE pending_2fa_user_id IS NOT NULL;

CREATE TABLE user_recovery_codes (
  id         SERIAL PRIMARY KEY,
  user_id    integer NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  code_hash  text NOT NULL,
  used_at    timestamptz,
  created_at timestamptz NOT NULL DEFAULT NOW()
);

CREATE INDEX user_recovery_codes_user_idx
  ON user_recovery_codes (user_id) WHERE used_at IS NULL;
```

### 2. `util/totp.go`

- `NewTOTPSecret() (string, error)`: generira 20 random bajtova i vraća ih kao base32 bez paddinga.
- `TOTPCode(secret string, step int64) (string, error)`
- `MatchTOTP(secret, code string, now time.Time) (step int64, ok bool)`:
  - provjerava korake `now-1`, `now` i `now+1`;
  - uspoređuje preko `subtle.ConstantTimeCompare`;
  - zaštita od ponovne upotrebe nije ovdje, nego u uvjetnom `UPDATE`-u (vidi 3.2).
- `TOTPURI(issuer, account, secret string) string`: vraća `otpauth://totp/...` URI.
- `EncryptTOTPSecret` i `DecryptTOTPSecret` (AES-GCM, ključ iz `util.Settings`).
- `NewRecoveryCodes(n int)` i `NormalizeRecoveryCode(s string)`. Hash i provjera koriste
  postojeće `util.HashPassword` i `util.VerifyPassword`.

### 2a. Pravilo zaključavanja (vrijedi za sve tokove)

Postojeći kod (blokiranje, brisanje korisnika, reset i promjena lozinke) prvo
zaključava ili mijenja red u `users`, a zatim mijenja ili briše `sessions`. Svi
novi tokovi moraju poštovati **isti redoslijed: `users` → `sessions` →
`user_recovery_codes`**, inače paralelni zahtjevi mogu ući u deadlock.

Kad se ID korisnika saznaje iz sesije (drugi korak prijave):
1. pročitati sesiju **bez** zaključavanja;
2. zaključati korisnika (`FOR UPDATE`);
3. zaključati sesiju (`FOR UPDATE`) i **ponovno** provjeriti da `pending_2fa_user_id`
   još pokazuje na tog korisnika i da pending nije istekao. Ako ne pokazuje,
   slijedi rollback i redirect na `/login`.

### 3. Login u dva koraka (`routes/auth.go`)

#### 3.1 `POST /auth`: prelazak u pending stanje

`authenticateUser` provjerava lozinku bez zaključavanja, pa između provjere i upisa
pendinga može proći reset lozinke. Taj reset bi obrisao pendinge, a ovaj zahtjev bi
zatim napravio novi. Zato se nakon uspješne provjere lozinke u **jednoj
transakciji** radi sljedeće:

1. `SELECT password, totp_enabled_at FROM users WHERE id = $1 AND blocked_at IS NULL AND email_verified = true FOR UPDATE`.
   Ako se `password` razlikuje od hasha koji je upravo provjeren, prijava se odbija
   kao neispravna. `authenticateUser` već vraća novi hash nakon rehasha, pa se
   uspoređuje s njim.
2. Ako korisnik ima `totp_enabled_at`, sesija se rotira jednim uvjetnim `UPDATE`-om
   (u suprotnom vidi kraj ovog odjeljka):

```sql
UPDATE sessions
SET uuid = $new_hash, user_id = 0,
    pending_2fa_user_id = $user_id, pending_2fa_at = NOW(), pending_2fa_failures = 0,
    pending_totp_secret = NULL, pending_totp_created_at = NULL
WHERE uuid = $current_hash
```

- Mora vrijediti `rowsAffected == 1`. Tek nakon commita se postavlja novi cookie.
- Ista provjera hasha pod zaključavanjem primjenjuje se i na **login bez 2FA**. Isti
  race već postoji u trenutnom kodu (prijava starom lozinkom može završiti nakon
  reseta), a `rotateSession` se ionako refaktorira da radi unutar transakcije.
- `user_id = 0` je obavezan. `/auth` trenutno prihvaća zahtjev i iz već prijavljene
  sesije, a sama činjenica da se `rotateSession` preskoči ne uklanja postojeću prijavu.
- Novi pokušaj lozinkom prepisuje prethodno pending stanje.
- **Prijava bez 2FA** (korisnik nema `totp_enabled_at`) rotira sesiju istim uvjetnim
  `UPDATE`-om, uz `user_id = $user_id` i **brisanje svih `pending_*` polja**
  (`pending_2fa_user_id`, `pending_2fa_at`, `pending_2fa_failures = 0`,
  `pending_totp_secret`, `pending_totp_created_at`). Primjer: korisnik započne 2FA
  prijavu za nalog A, a zatim se prijavi na nalog B bez 2FA. Postojeći
  `rotateSession` sam ne bi obrisao pending za A. Isto čišćenje radi i `logout`,
  jer briše cijeli red sesije.
- Redirect ide na `/login/2fa`, uz zadržavanje `next=/admin`.

Ako je `totpkey` nedostupan ili neispravan, a korisnik ima uključen 2FA, sustav se
ponaša **fail closed**:
- TOTP se ne može provjeriti, pa korisnik dobiva poruku da prijava kodom trenutno
  nije moguća i da može upotrijebiti recovery kod. Recovery kodovi ne ovise o ključu.
- Greška se zapisuje u log i u `admin_events` (`auth_2fa_key_error`).
- 2FA se nikad ne preskače.

Pri startu se zapisuje upozorenje u log ako ključ nedostaje, a postoje korisnici s
uključenim 2FA.

#### 3.2 `POST /login/2fa`: završetak prijave

**Prije transakcije (bez zaključavanja):**
1. Pročitati sesiju i dobiti `pending_2fa_user_id`. Ako pendinga nema, slijedi
   redirect na `/login`.
2. Ako je unesen recovery kod, pročitati neiskorištene kodove korisnika i bcryptom
   pronaći odgovarajući `code_id`. To se radi prije transakcije, jer je bcrypt spor.

**Jedna transakcija** (`runTransaction`), redoslijed prema 2a:

3. `SELECT ... FROM users WHERE id = $pending_user AND blocked_at IS NULL AND email_verified = true AND totp_enabled_at IS NOT NULL FOR UPDATE`.
4. `SELECT ... FROM sessions WHERE uuid = $current FOR UPDATE`, a zatim ponovna provjera:
   - `pending_2fa_user_id` je još isti;
   - `pending_2fa_at` nije stariji od 5 minuta;
   - `pending_2fa_failures < 5`.

   Ako provjera iz koraka 3 ili 4 ne prođe, pending se čisti, slijedi commit i
   redirect na `/login`.
5. Provjera i potrošnja koda:
   - **TOTP**: `MatchTOTP` vraća `step`, a zatim se izvršava
     `UPDATE users SET totp_last_step = $step WHERE id = $1 AND totp_last_step < $step`.
     Obavezno je `rowsAffected == 1`, inače je kod već iskorišten.
   - **Recovery kod**: `UPDATE user_recovery_codes SET used_at = NOW() WHERE id = $code_id AND user_id = $1 AND used_at IS NULL`.
     Obavezno je `rowsAffected == 1`.
6. **Ako je kod ispravan**, u istoj transakciji se:
   - rotira sesija: `UPDATE sessions SET uuid = $new, user_id = $user, pending_* = NULL/0 WHERE uuid = $current`
     (obavezno `rowsAffected == 1`);
   - upisuje `admin_events` (`auth_login`, a za recovery kod i `auth_2fa_recovery_used`);
   - slijedi commit, a zatim se postavlja cookie i radi redirect na `/posts` ili `/admin`.
   - `rotateSession` treba refaktorirati tako da prima `sqlExecer`/`*sqlx.Tx`, a
     cookie se postavlja zasebno, nakon commita.
   - Ako bilo koji korak ne uspije, slijedi **rollback** i kod ostaje nepotrošen.
7. **Ako je kod neispravan**, u **istoj** transakciji, dok su redovi još zaključani:
   - izvršava se `pending_2fa_failures = pending_2fa_failures + 1`;
   - ako brojač dosegne 5, pending se briše i korisnik mora ponovno unijeti lozinku;
   - zapisuje se `auth_2fa_failed`;
   - transakcija se **commita**, a ne rollbacka, i vraća se poruka o grešci.
   - Funkcija transakcije zato vraća ishod (`ok`, `wrongCode`, `expired`) umjesto
     greške. Greška znači samo kvar baze i vodi u rollback.

#### 3.3 Rate limiting

Postojeći `requestLimiter` drži stanje u memoriji i resetira se pri restartu. Zato se
oslanjamo i na brojač `pending_2fa_failures` u bazi.

| Ključ | Limit | Prozor | Rute |
|---|---|---|---|
| `2fa:ip:<ip>` | 30 | 15 min | `POST /login/2fa` |
| `2fa:user:<pending_2fa_user_id>` | 10 | 15 min | `POST /login/2fa` |
| `2fa-manage:user:<user_id>` | 10 | 15 min | `POST /account/2fa/*` |

- Ključ po korisniku se izvodi iz **serverskog** pending stanja, a ne iz forme ni iz
  IP adrese. Tako vrijedi i kroz nove sesije i kroz ponovljene prijave lozinkom.
  `loginAccountKey` sadrži IP, pa nije prikladan obrazac.
- Rate limit middleware za `/login/2fa` mora pročitati pending iz sesije. Ako
  pendinga nema, primjenjuje se samo IP pravilo.
- Uspješna prijava **ne** resetira ključ po korisniku.

#### 3.4 `GET /login/2fa`

- Prikazuje `templates/login-2fa.html`: polje za kod i link „Koristi recovery kod”.
- Bez važećeg pendinga slijedi redirect na `/login`.
- Odgovor ima `Cache-Control: no-store` i `data.HideThirdPartyScripts = true`.

### 4. Uključivanje i isključivanje (prijavljeni korisnik)

Sve promjene faktora traže **trenutnu lozinku**, zbog ponovne autentifikacije (OWASP).
Sve stranice imaju `Cache-Control: no-store` i `HideThirdPartyScripts = true`, jer
`templates/header.html` inače učitava Analytics i AdSense.

- `GET /account/2fa`:
  - ako 2FA nije uključen, prikazuje QR kod, secret kao tekst i polja za lozinku i
    prvi kod;
    - setup secret se sprema šifriran u `sessions.pending_totp_secret` i vrijedi
      **10 minuta** od `pending_totp_created_at`;
    - dok ne istekne, osvježavanje stranice prikazuje **isti** secret. Nakon isteka
      generira se novi;
  - ako je 2FA uključen, prikazuje status, broj preostalih recovery kodova, formu za
    isključivanje i formu za nove recovery kodove.
- `POST /account/2fa/enable` traži lozinku i kod iz setup secreta. Sve se radi u
  jednoj transakciji, redoslijedom iz 2a:
  1. **users**: zaključavanje korisnika i provjera lozinke prema zaključanom hashu,
     a zatim `UPDATE users SET totp_secret, totp_enabled_at = NOW(), totp_last_step = $step WHERE id = $1 AND totp_enabled_at IS NULL`;
  2. **sessions**: zaključavanje trenutne sesije, provjera da setup secret nije istekao,
     zatim brisanje setup secreta iz sesije i brisanje ostalih sesija i pendinga
     korisnika (vidi 6);
  3. **user_recovery_codes**: upis recovery kodova;
  4. zapis `2fa_enabled`.

  Setup secret se može pročitati iz sesije prije transakcije, kako bi se provjerio
  kod. Unutar transakcije, nakon zaključavanja sesije, ponovno se provjerava da je
  secret isti.

  Recovery kodovi se nakon toga prikazuju **samo jednom**, u odgovoru na POST, a
  ne putem redirecta.
- `POST /account/2fa/disable` traži lozinku i TOTP ili recovery kod. Sve se radi u
  jednoj transakciji, redoslijedom iz 2a:
  1. **users**: zaključavanje korisnika, provjera lozinke, potrošnja TOTP koraka i
     brisanje secreta (`totp_secret = NULL`, `totp_enabled_at = NULL`);
  2. **sessions**: poništavanje ostalih sesija i pendinga (vidi 6);
  3. **user_recovery_codes**: potrošnja unesenog recovery koda (ako je korišten) i
     brisanje svih kodova;
  4. zapis `2fa_disabled`.
- `POST /account/2fa/recovery-codes` traži lozinku i TOTP kod. Sve se radi u
  **jednoj transakciji**, redoslijedom iz 2a:
  1. zaključavanje korisnika i provjera lozinke prema zaključanom hashu;
  2. potrošnja TOTP koraka uvjetnim `UPDATE`-om nad `totp_last_step`;
  3. poništavanje ostalih sesija i pendinga korisnika (vidi 6);
  4. brisanje starih kodova i upis novih;
  5. zapis `2fa_recovery_regenerated`.

  Novi kodovi se prikazuju **samo jednom**, u odgovoru na POST, kao kod uključivanja.
- Enable, disable i regenerate provjeravaju lozinku na isti način kao 3.1: nakon
  `FOR UPDATE` nad korisnikom, prema zaključanom hashu.
- Link na `/account/2fa` dodati u `navbar.html` ili na stranicu za promjenu lozinke.
- Dodati prijevode novih tekstova u `util/lang.go`.

### 5. Admin reset

- Ruta je `POST /admin/users/:id/2fa/reset` s `adminOnly`, uz istu CSRF zaštitu kao
  ostale admin POST rute.
- Admin potvrđuje vlastitom lozinkom i upisuje razlog (obavezan).
- Kao kod blokiranja, ne vrijedi za admin naloge. Admin svoj 2FA mijenja kroz
  `/account/2fa`.
- Sve se radi u jednoj transakciji, redoslijedom iz 2a:
  1. **users**: zaključavanje korisnika i brisanje secreta;
  2. **sessions**: poništavanje svih sesija i pendinga korisnika;
  3. **user_recovery_codes**: brisanje svih kodova;
  4. zapis `admin_2fa_reset` s `actor_user_id` i razlogom u `detail`.
- U `admin-user.html` dodati status 2FA i formu za reset.

### 6. Poništavanje sesija mora obuhvatiti pendinge

Pending sesije imaju `user_id = 0`, pa ih postojeći `DELETE ... WHERE user_id = $1`
ne briše. Uvjet se proširuje uvijek **u zagradama**:

```sql
-- sve sesije korisnika (reset lozinke, blokiranje, brisanje, admin reset 2FA)
DELETE FROM sessions
WHERE (user_id = $1 OR pending_2fa_user_id = $1);

-- sve osim trenutne (promjena lozinke, enable, disable, regenerate)
DELETE FROM sessions
WHERE (user_id = $1 OR pending_2fa_user_id = $1)
  AND uuid <> $2;
```

Bez zagrada bi se uvjet pročitao kao `user_id = $1 OR (pending_2fa_user_id = $1 AND uuid <> $2)`
i obrisao bi i trenutnu prijavljenu sesiju. Uvjet se koristi na sljedećim mjestima:

- reset lozinke: `routes/password.go:147`;
- promjena lozinke: `routes/password.go:182`;
- blokiranje i brisanje korisnika: `routes/admin.go:328`, `routes/admin.go:390`;
- novi 2FA tokovi: enable, disable, regenerate i admin reset.

Uz to, drugi korak (3.2, točka 3) iznova provjerava blokadu, verifikaciju i aktivan
2FA. **Promjenu lozinke ta provjera ne otkriva.** Zaštitu od pendinga nastalog
nakon reseta daje provjera hasha pod zaključavanjem u 3.1, a ne ova točka. Zato su
oba mehanizma obavezna.

## Sigurnosni zahtjevi

- Lozinka bez drugog faktora **nikad** ne daje sesiju s `user_id > 0`.
- Prelazak u pending i završetak prijave uvijek rotiraju session token.
- TOTP korak i recovery kod troše se atomski, uvjetnim `UPDATE`-om s provjerom
  `rowsAffected`. Dva paralelna zahtjeva ne mogu oba uspjeti (NIST SP 800-63B,
  jednokratnost OTP-a).
- Potrošnja koda, potrošnja pendinga, rotacija sesije i `auth_login` su jedna
  transakcija. Ako bilo koji korak ne uspije, slijedi rollback i cookie se ne postavlja.
- Svi tokovi zaključavaju redove redoslijedom `users` → `sessions` → `user_recovery_codes` (2a).
- Pending (i obična prijava) nastaje samo ako se lozinka nije promijenila od
  provjere (3.1).
- Neuspješan pokušaj se broji pod istim zaključavanjem pod kojim se provjerava
  brojač, pa paralelni zahtjevi ne dobivaju dodatne pokušaje.
- Reset lozinke ne zaobilazi 2FA. Korisnik ostaje odjavljen i mora proći oba koraka.
- Stranice s QR kodom, recovery kodovima i unosom drugog faktora imaju `no-store` i
  `HideThirdPartyScripts`.
- Secret i recovery kodovi se nikad ne logiraju. Secret se nakon uključivanja nikad
  ne vraća u plain obliku.
- Ako nema ključa, sustav se ponaša fail closed (vidi 3.1).

## Testovi

- `util/totp_test.go`:
  - test vektori iz RFC 6238 (SHA1);
  - tolerancija ±1 korak, odbijanje ±2;
  - roundtrip šifriranja i dešifriranja;
  - neispravan ključ ili ciphertext;
  - normalizacija i entropija recovery kodova.
- `routes/two_factor_test.go` (sqlmock, po uzoru na `auth_test.go`):
  - lozinka OK uz uključen 2FA: sesija ima `user_id = 0`, token je rotiran i slijedi redirect na `/login/2fa`;
  - `/auth` iz **već prijavljene** sesije za korisnika s 2FA: prethodna prijava se uklanja;
  - lozinka OK uz isključen 2FA: login radi kao do sada;
  - započet 2FA pending za nalog A, a zatim prijava na nalog B bez 2FA: sva
    `pending_*` polja su obrisana;
  - promjena lozinke, enable, disable i regenerate brišu ostale sesije i pendinge,
    ali **ne** trenutnu sesiju;
  - ispravan kod prijavljuje korisnika, neispravan povećava `pending_2fa_failures`,
    a nakon 5 neuspjeha pending se briše;
  - istekli pending se odbija;
  - ponovljeni TOTP korak se odbija; recovery kod radi samo jednom;
  - **rollback**: ako rotacija sesije ili `admin_events` ne uspije, kod nije potrošen
    i cookie nije postavljen;
  - pending za blokiranog korisnika ili korisnika bez 2FA ne prolazi;
  - enable, disable i regenerate bez ispravne lozinke ne prolaze;
  - setup secret se ponovno koristi unutar 10 minuta, a nakon isteka se mijenja;
  - nedostajući ključ: TOTP ne prolazi (fail closed), recovery kod prolazi;
  - admin reset: `adminOnly`, potvrda lozinkom, `actor_user_id` u događaju;
  - `HideThirdPartyScripts` na svim 2FA stranicama.
- PostgreSQL integracijski testovi (`routes/postgres_integration_test.go`, `migrations/postgres_test.go`):
  - migracija 030 i `SchemaVersion = 30`;
  - reset lozinke, promjena lozinke, blokiranje i admin reset 2FA poništavaju
    pending sesije (`pending_2fa_user_id`);
  - **paralelni testovi** (vidi niže):
    - isti TOTP kod i isti recovery kod iz dvije sesije: točno jedan zahtjev uspije;
    - 5+ paralelnih pogrešnih kodova na isti pending: ukupno se prihvati najviše 5
      pokušaja, pending je obrisan, a `auth_2fa_failed` ima točno 5 zapisa;
    - drugi korak prijave paralelno s blokiranjem ili resetom lozinke: nema deadlocka
      (test s timeoutom), a korisnik na kraju nije prijavljen;
    - `/auth` sa starom lozinkom paralelno s resetom lozinke: na kraju ne postoji
      pending ni prijavljena sesija.

**Infrastruktura za paralelne testove**

`internal/testdb/testdb.go:82` postavlja `SetMaxOpenConns(1)`, pa bi se transakcije
izvršavale redom i test ne bi provjeravao konkurentnost.

- Dodati opciju, npr. `testdb.Open(t, testdb.WithMaxOpenConns(4))`, ili u testu
  pozvati `SetMaxOpenConns(4)` na vraćenoj konekciji.
- Preklapanje treba biti **deterministično**, a ne prepušteno slučaju:
  1. treća konekcija otvori transakciju i zaključa red korisnika
     (`SELECT ... FOR UPDATE`);
  2. pokrenu se oba zahtjeva; oba stanu na tom zaključavanju (provjera preko
     `pg_stat_activity`, `wait_event_type = 'Lock'`);
  3. treća transakcija se rollbacka i zahtjevi se natječu.
- Svaki paralelni test ima `context` timeout, kako bi deadlock bio vidljiv kao
  neuspjeh, a ne kao test koji visi.

## Verifikacija

1. `go test ./...`
2. Skill `run-goexpenses`, u headless Chromiumu:
   - uključiti 2FA (kod izračunati iz secreta u testu);
   - odjaviti se, prijaviti lozinkom pa kodom;
   - prijaviti se recovery kodom;
   - isključiti 2FA;
   - provjeriti da 2FA stranice ne učitavaju gtag ni AdSense.
3. Zaustaviti server pomoću `stop.sh`.

## Izvan opsega

- WebAuthn / passkeys
- SMS i e-mail kodovi
- „Zapamti ovaj uređaj”
- Rotacija `totpkey` ključa
- Obavezni 2FA za admine (može se dodati kasnije kao zasebna postavka)

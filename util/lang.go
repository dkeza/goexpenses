package util

func GetLangText(t string, l string) string {
	r := ""
	switch l {
	case "RS":
		r = AppLang[t].RS
	case "SR":
		r = AppLang[t].SR
	case "DE":
		r = AppLang[t].DE
	}
	if r == "" {
		r = t
	}
	return r
}

// Description of LangText object.
type LangText struct {
	RS string
	SR string
	DE string
}

// Language texts.
var AppLang = map[string]LangText{}

func init() {
	AppLang["Main navigation"] = LangText{"Glavna navigacija", "Главна навигација", "Hauptnavigation"}
	AppLang["Toggle navigation"] = LangText{"Otvori ili zatvori navigaciju", "Отвори или затвори навигацију", "Navigation ein- oder ausblenden"}
	AppLang["Toggle color theme"] = LangText{"Promeni temu", "Промени тему", "Farbschema wechseln"}
	AppLang["Confirm deletion"] = LangText{"Potvrda brisanja", "Потврда брисања", "Löschen bestätigen"}
	AppLang["Actions"] = LangText{"Akcije", "Радње", "Aktionen"}
	AppLang["Summary"] = LangText{"Pregled", "Преглед", "Übersicht"}
	AppLang["Apply filter"] = LangText{"Primeni filter", "Примени филтер", "Filter anwenden"}
	AppLang["Reset password"] = LangText{"Poništi lozinku", "Поништи лозинку", "Kennwort zurücksetzen"}
	AppLang["Reset link would be sent to Your E-Mail. Please click on received link to proceed."] = LangText{"Link za poništavanje lozinke biće poslat na vašu E-Mail adresu.", "Линк за поништавање лозинке биће послат на вашу Е-Пошту.", "Ein Link zum Zurücksetzen wird an Ihre E-Mail-Adresse gesendet."}
	AppLang["Expenses"] = LangText{"Troškovi", "Трошкови", "Kosten"}
	AppLang["Expense"] = LangText{"Тrоšak", "Трошак", "Kosten"}
	AppLang["Incomes"] = LangText{"Prihodi", "Приходи", "Einkommen"}
	AppLang["Income"] = LangText{"Prihod", "Приход", "Einkommen"}
	AppLang["Home"] = LangText{"Početak", "Почетак", "Anfang"}
	AppLang["Posts"] = LangText{"Stavke", "Ставке", "Buchungen"}
	AppLang["Posts pagination"] = LangText{"Stranice stavki", "Странице ставки", "Buchungsseiten"}
	AppLang["Newer"] = LangText{"Novije", "Новије", "Neuere"}
	AppLang["Older"] = LangText{"Starije", "Старије", "Ältere"}
	AppLang["Hi"] = LangText{"Zdravo", "Здраво", "Hallo"}
	AppLang["Language"] = LangText{"Jezik", "Језик", "Sprache"}
	AppLang["Simple Expenses App"] = LangText{"Jednostavna aplikacija za evidenciju troškova", "Једноставна апликација за контролу трошкова", "Einfache Ausgaben App"}
	AppLang["Register, and create unlimited accounts."] = LangText{"Registrujte se i vodite neograničen broj računa.", "Региструјте се и водите неограничен број рачуна.", "Registrieren und unbegrenzte Konten erstellen."}
	AppLang["Track Your incomes and expenses."] = LangText{"Pratite vaše prihode i rashode.", "Пратите ваше приходе и расходе.", "Verfolgen Sie Ihre Einnahmen und Ausgaben."}
	AppLang["Account"] = LangText{"Račun", "Рачун", "Konto"}
	AppLang["New account"] = LangText{"Novi račun", "Нови рачун", "Neues Konto"}
	AppLang["Description"] = LangText{"Opis", "Опис", "Beschreibung"}
	AppLang["Save"] = LangText{"Snimi", "Сними", "Speichern"}
	AppLang["Delete"] = LangText{"Obriši", "Обриши", "Löschen"}
	AppLang["Edit"] = LangText{"Izmeni", "Измени", "Ändern"}
	AppLang["Expenses App"] = LangText{"Aplikacija za evidenciju troškova", "Апликација за евиденцију трошкова", "Kosten App"}
	AppLang["Login"] = LangText{"Prijava", "Пријава", "Anmelden"}
	AppLang["Logout"] = LangText{"Odjava", "Одјава", "Abmelden"}
	AppLang["User name"] = LangText{"Korisničko ime", "Корисничко име", "Benutzername"}
	AppLang["Password"] = LangText{"Lozinka", "Лозинка", "Kennwort"}
	AppLang["Login me in"] = LangText{"Prijavi me", "Пријави ме", "Melde mich an"}
	AppLang["Register"] = LangText{"Registruj se", "Региструј се", "Neu registrieren"}
	AppLang["Income type"] = LangText{"Vrsta prihoda", "Врста прихода", "Einkommen Typ"}
	AppLang["Amount"] = LangText{"Iznos", "Износ", "Betrag"}
	AppLang["Saldo"] = LangText{"Saldo", "Салдо", "Saldo"}
	AppLang["Expense type"] = LangText{"Vrsta troška", "Врста трошка", "Kosten Typ"}
	AppLang["New income"] = LangText{"Novi prihod", "Нови приход", "Neuer Einkommen"}
	AppLang["Date"] = LangText{"Datum", "Датум", "Datum"}
	AppLang["Update expense"] = LangText{"Izmeni trošak", "Измени трошак", "Kosten ändern"}
	AppLang["Name"] = LangText{"Ime", "Име", "Name"}
	AppLang["E-Mail"] = LangText{"E-Mail", "Е-Пошта", "E-Mail"}
	AppLang["Create account"] = LangText{"Napravi račun", "Направи рачун", "Konto erstellen"}
	AppLang["Enter new income post"] = LangText{"Unesi novu stavku prihoda", "Унеси нову ставку прихода", "Neue Einkommenbuchung erstellen"}
	AppLang["Update income"] = LangText{"Izmeni prihod", "Измени приход", "Einkommen ändern"}
	AppLang["Update post"] = LangText{"Izmeni stavku", "Измени ставку", "Buchung ändern"}
	AppLang["My account"] = LangText{"Moj račun", "Мој рачун", "Mein Konto"}
	AppLang["Invalid description!"] = LangText{"Neispravan opis!", "Неисправан опис!", "Ungültige Beschreibung!"}
	AppLang["Invalid amount!"] = LangText{"Neispravan iznos!", "Неисправан износ!", "Ungültiger Betrag!"}
	AppLang["Invalid date!"] = LangText{"Neispravan datum!", "Неисправан датум!", "Ungültiges Datum!"}
	AppLang["Do You really want delete this record?"] = LangText{"Da li si siguran da želiš da obrišeš ovu stavku?", "Да ли си сигуран да желиш да обришеш ову ставку?", "Bist du sicher, dass du willst diese Buchung löschen?"}
	AppLang["Yes"] = LangText{"Da", "Да", "Ja"}
	AppLang["No"] = LangText{"Ne", "Не", "Nein"}
	AppLang["Saved"] = LangText{"Snimljeno", "Снимљено", "Gespeichert"}
	AppLang["Change password"] = LangText{"Promeni lozinku", "Промени лозинку", "Kennwort ändern"}
	AppLang["Repeat password"] = LangText{"Ponovi lozinku", "Понови лозинку", "Kennwort wiederholen"}
	AppLang["Invalid password!"] = LangText{"Neispravna lozinka", "Неисправна лозинка", "Ungültiges Kennwort"}
	AppLang["Invalid name!"] = LangText{"Neispravno ime!", "Неисправно име!", "Ungültiger Name!"}
	AppLang["Invalid E-Mail!"] = LangText{"Neispravna E-Mail adresa!", "Неисправна Е-Пошта адреса!", "Ungültige E-Mail-Adresse!"}
	AppLang["Invalid user name!"] = LangText{"Korisničko ime mora imati 3–64 slova, broja ili znaka . _ -", "Корисничко име мора имати 3–64 слова, броја или знака . _ -", "Der Benutzername muss aus 3–64 Buchstaben, Zahlen oder . _ - bestehen."}
	AppLang["Password must contain between 10 and 72 characters."] = LangText{"Lozinka mora imati između 10 i 72 znaka.", "Лозинка мора имати између 10 и 72 знака.", "Das Kennwort muss zwischen 10 und 72 Zeichen enthalten."}
	AppLang["User name is already in use."] = LangText{"Korisničko ime je već zauzeto.", "Корисничко име је већ заузето.", "Der Benutzername wird bereits verwendet."}
	AppLang["E-Mail is already in use."] = LangText{"E-Mail adresa je već u upotrebi.", "Е-Пошта адреса је већ у употреби.", "Die E-Mail-Adresse wird bereits verwendet."}
	AppLang["From"] = LangText{"Od", "Од", "Von"}
	AppLang["To"] = LangText{"Do", "До", "Bis"}
	AppLang["Filter"] = LangText{"Filter", "Филтер", "Filter"}
	AppLang["Cancel"] = LangText{"Odustani", "Одустани", "Abbrechen"}
	AppLang["January"] = LangText{"Januar", "Јануар", "Januar"}
	AppLang["February"] = LangText{"Februar", "Фебруар", "Februar"}
	AppLang["March"] = LangText{"Mart", "Март", "März"}
	AppLang["April"] = LangText{"April", "Април", "April"}
	AppLang["May"] = LangText{"Maj", "Мај", "Mai"}
	AppLang["June"] = LangText{"Jun", "Јун", "Juni"}
	AppLang["July"] = LangText{"Jul", "Јул", "Juli"}
	AppLang["August"] = LangText{"Avgust", "Август", "August"}
	AppLang["September"] = LangText{"Septembar", "Септембар", "September"}
	AppLang["October"] = LangText{"Oktobar", "Октобар", "Oktober"}
	AppLang["November"] = LangText{"Novembar", "Новембар", "November"}
	AppLang["December"] = LangText{"Decembar", "Децембар", "Dezember"}
	AppLang["Jan"] = LangText{"Jan", "Јан", "Jan"}
	AppLang["Feb"] = LangText{"Feb", "Феб", "Feb"}
	AppLang["Mar"] = LangText{"Mar", "Мар", "Mär"}
	AppLang["Apr"] = LangText{"Apr", "Апр", "Apr"}
	AppLang["May"] = LangText{"Maj", "Мај", "Mai"}
	AppLang["Jun"] = LangText{"Jun", "Јун", "Juni"}
	AppLang["Jul"] = LangText{"Jul", "Јул", "Juli"}
	AppLang["Aug"] = LangText{"Aug", "Ауг", "Aug"}
	AppLang["Sep"] = LangText{"Sep", "Сеп", "Sep"}
	AppLang["Oct"] = LangText{"Okt", "Окт", "Okt"}
	AppLang["Nov"] = LangText{"Nov", "Нов", "Nov"}
	AppLang["Dec"] = LangText{"Dec", "Дец", "Dez"}
	AppLang["Monday"] = LangText{"Ponedeljak", "Понедељак", "Montag"}
	AppLang["Tuesday"] = LangText{"Utorak", "Уторак", "Dienstag"}
	AppLang["Wednesday"] = LangText{"Sreda", "Среда", "Mitwoch"}
	AppLang["Thursday"] = LangText{"Četvrtak", "Четвртак", "Donnerstag"}
	AppLang["Friday"] = LangText{"Petak", "Петак", "Freitag"}
	AppLang["Saturday"] = LangText{"Subota", "Субота", "Samstag"}
	AppLang["Sunday"] = LangText{"Nedelja", "Недеља", "Sonntag"}
	AppLang["Mo"] = LangText{"Po", "По", "Mo"}
	AppLang["Tu"] = LangText{"Ut", "Ут", "Di"}
	AppLang["We"] = LangText{"Sr", "Ср", "Mi"}
	AppLang["Th"] = LangText{"Če", "Че", "Do"}
	AppLang["Fr"] = LangText{"Pe", "Пе", "Fr"}
	AppLang["Sa"] = LangText{"Su", "Су", "Sa"}
	AppLang["Su"] = LangText{"Ne", "Не", "So"}
	AppLang["Today"] = LangText{"Danas", "Данас", "Heute"}
	AppLang["Clear"] = LangText{"Poništi", "Поништи", "Löschen"}
	AppLang["Close"] = LangText{"Zatvori", "Затвори", "Schliießen"}
	AppLang["Unknown user or invalid password!"] = LangText{"Nepoznat korisnik ili neispravna lozinka!", "Непознат корисник или неисправна лозинка!", "Unbekannter Benutzer oder ungültiges Kennwort!"}
	AppLang["I forgot my password"] = LangText{"Zaboravio sam lozinku", "Заборавио сам лозинку", "Ich habe mein Kennwort vergessen"}
	AppLang["Not allowed to reset password!"] = LangText{"Nije dozvoljeno poništavanje lozinke", "Није дозвољено поништавање лозинке!", "Es ist nicht erlaubt das Kennwort zu löschen!"}
	AppLang["reset password"] = LangText{"poništavanje lozinke", "поништавање лозинке", "löschen Kennwort"}
	AppLang["Click to this link to reset password:"] = LangText{"Klikni na ovaj link da poništiš lozinku:", "Кликни на овај линк да поништиш лозинку:", "Bitte klicken an diesen Link um die Kennwort zu löschen:"}
	AppLang["Unknown E-Mail!"] = LangText{"Nepoznati E-Mail!", "Непознати Е-Маил!", "Unbekanntes E-Mail!"}
	AppLang["Error when accesing to database!"] = LangText{"Greška pri pristupu bazi podataka!", "Грешка при приступу бази података!", "Fehler bei den Zugriff zu der Datenbank!"}
	AppLang["E-Mail not sent!"] = LangText{"E-Mail nije poslat!", "Е-Маил није послат!", "E-Mail war nicht gesendet!"}
	AppLang["E-Mail sent!"] = LangText{"E-Mail je poslat!", "Е-Маил је послат!", "E-Mail war gesendet!"}
	AppLang["If the E-Mail exists, a reset link has been sent."] = LangText{"Ako E-Mail postoji, link za poništavanje lozinke je poslat.", "Ако Е-Маил постоји, линк за поништавање лозинке је послат.", "Falls die E-Mail existiert, wurde ein Link zum Zurücksetzen gesendet."}
	AppLang["Invalid token!"] = LangText{"Neispravan token!", "Неисправан токен!", "Ungültiger Token!"}
	AppLang["I forgot my password"] = LangText{"Zaboravio sam lozinku", "Заборавио сам лозинку", "Ich habe meine Kennwort vergessen"}
	AppLang["Reset filter"] = LangText{"Poništi filter", "Поништи филтер", "Filter löschen"}
	AppLang["Fee"] = LangText{"Provizija", "Провизија", "Gebühren"}
	AppLang["Changes not saved, because of invalid input data!"] = LangText{"Promene nisu zapamćene zbog neispravnih ulaznih podataka!", "Промене нису запамћене због несиправних улазних података!", "Änderungen sind nicht gespeichert, weil es ungültigen eingang Daten gibt!"}
	AppLang["Invalid expense!"] = LangText{"Neispravan trošak!", "Неисправан трошак!", "Ungültiger Kosten!"}
	AppLang["Invalid income!"] = LangText{"Neispravan prihod!", "Неисправан приход!", "Ungültiger Einkommen!"}
	AppLang["Timestamp"] = LangText{"Kreiran", "Креиран", "Zeitstempel"}
}

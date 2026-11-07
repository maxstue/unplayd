const messages = new Map<string, string>([
  ["form_password_incorrect", "Anmeldung nicht möglich. Prüfe E-Mail-Adresse und Passwort."],
  ["form_identifier_not_found", "Anmeldung nicht möglich. Prüfe E-Mail-Adresse und Passwort."],
  [
    "form_identifier_exists",
    "Für diese E-Mail-Adresse besteht bereits ein Konto. Melde dich an oder nutze „Passwort vergessen?“ auf der Anmeldeseite.",
  ],
  ["form_code_incorrect", "Der Code stimmt nicht. Bitte prüfe ihn und versuche es erneut."],
  ["verification_expired", "Der Code ist abgelaufen. Fordere einen neuen an."],
  [
    "form_password_pwned",
    "Dieses Passwort wurde in einem Datenleck gefunden. Bitte wähle ein anderes.",
  ],
  [
    "form_password_length_too_short",
    "Das Passwort ist zu kurz. Bitte verwende mindestens 15 Zeichen.",
  ],
  [
    "form_password_validation_failed",
    "Das Passwort erfüllt die Sicherheitsanforderungen nicht. Verwende ein längeres Passwort.",
  ],
  ["too_many_requests", "Zu viele Versuche. Bitte warte einen Moment und versuche es erneut."],
  [
    "captcha_invalid",
    "Die Sicherheitsprüfung konnte nicht abgeschlossen werden. Bitte versuche es erneut.",
  ],
  ["session_exists", "Du bist bereits angemeldet. Öffne dein Konto über die Navigation."],
]);

export function authErrorMessage(cause: unknown): string {
  if (cause && typeof cause === "object" && "errors" in cause && Array.isArray(cause.errors)) {
    const first: unknown = cause.errors[0];
    if (first && typeof first === "object" && "code" in first && typeof first.code === "string") {
      return messages.get(first.code) ?? "Das hat gerade nicht geklappt. Bitte versuche es erneut.";
    }
  }
  return "Die Anmeldung ist gerade nicht erreichbar. Bitte versuche es erneut.";
}

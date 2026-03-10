package accountability

import "strings"

const commitmentTable = "commitments"

const commitmentColumns = "id,user_id,task,created_at,deadline,snoozed_until,last_checkin_at,last_checkin_text,checkin_quiet_until,reminder_count,policy_preset,policy_engine,policy_config,status,proof_metadata,updated_at"

const baseCommitmentSelect = "SELECT " + commitmentColumns + " FROM " + commitmentTable

const insertCommitmentSQL = `INSERT INTO commitments(
	user_id,
	task,
	goal_slug,
	created_at,
	deadline,
	snoozed_until,
	last_reminder_at,
	last_checkin_at,
	last_checkin_text,
	checkin_quiet_until,
	reminder_count,
	policy_preset,
	policy_engine,
	policy_config,
	status,
	updated_at
) VALUES(?, ?, '', ?, ?, '', '', '', '', '', 0, ?, ?, ?, 'pending', ?);`

const updateCommitmentCanceledSQL = `UPDATE commitments SET status='canceled',updated_at=? WHERE id=? AND status='pending';`

const updateCommitmentSnoozedSQL = `UPDATE commitments SET snoozed_until=?,updated_at=? WHERE id=? AND status='pending';`

const updateCommitmentCheckInSQL = `UPDATE commitments SET last_checkin_at=?,last_checkin_text=?,checkin_quiet_until=?,updated_at=? WHERE id=? AND status='pending';`

const updateCommitmentProofSQL = `UPDATE commitments SET status=?,proof_metadata=?,snoozed_until='',updated_at=? WHERE id=? AND status='pending';`

const updateCommitmentReminderSQL = `UPDATE commitments SET last_reminder_at=?,reminder_count=reminder_count+1 WHERE id=? AND status='pending';`

const updateOverdueCommitmentsSQL = `UPDATE commitments SET status='failed',updated_at=? WHERE status='pending' AND deadline <= ?;`

func commitmentSelect(whereClause string) string {
	if strings.TrimSpace(whereClause) == "" {
		return baseCommitmentSelect
	}
	return baseCommitmentSelect + " " + strings.TrimSpace(whereClause)
}

func overdueNeedingReminderQuery(includeExpiryGrace bool) string {
	conditions := []string{
		"WHERE status='pending'",
		"AND deadline <= ?",
	}
	if includeExpiryGrace {
		conditions = append(conditions, "AND deadline > ?")
	}
	conditions = append(
		conditions,
		"AND (snoozed_until='' OR snoozed_until <= ?)",
		"AND (checkin_quiet_until='' OR checkin_quiet_until <= ?)",
		"AND (last_reminder_at='' OR last_reminder_at <= ?)",
	)
	return commitmentSelect(strings.Join(conditions, " ")) + ";"
}

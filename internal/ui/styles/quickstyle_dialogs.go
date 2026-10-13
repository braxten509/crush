package styles

import (
	"charm.land/lipgloss/v2"
)

// quickStyleDialogs sets the dialog styles.
func quickStyleDialogs(s *Styles, o quickStyleOpts, base lipgloss.Style) {
	// Dialog styles
	s.Dialog.Title = base.Padding(0, 1).Foreground(o.primary)
	s.Dialog.TitleText = base.Foreground(o.primary)
	s.Dialog.TitleError = base.Foreground(o.destructive)
	s.Dialog.TitleAccent = base.Foreground(o.success).Bold(true)
	s.Dialog.TitleLineBase = lipgloss.NewStyle()
	s.Dialog.TitleGradFromColor = o.primary
	s.Dialog.TitleGradToColor = o.secondary

	// Dialog.ListItem (commands, reasoning, models). The info column holds
	// secondary hints like keybind shortcuts, so mute it when blurred and
	// keep it readable on the focused row.
	s.Dialog.ListItem.InfoBlurred = lipgloss.NewStyle().Foreground(o.fgMostSubtle)
	s.Dialog.ListItem.InfoFocused = lipgloss.NewStyle().Foreground(o.fgBase)

	// Dialog.Models
	s.Dialog.Models.ConfiguredText = lipgloss.NewStyle().Foreground(o.fgMostSubtle)

	// Dialog.Permissions
	s.Dialog.Permissions.KeyText = lipgloss.NewStyle().Foreground(o.fgMoreSubtle)
	s.Dialog.Permissions.ValueText = lipgloss.NewStyle().Foreground(o.fgBase)
	s.Dialog.Permissions.ParamsBg = o.bgLessVisible

	// Dialog.Quit
	s.Dialog.Quit.Content = lipgloss.NewStyle().Foreground(o.fgBase)
	s.Dialog.Quit.Hint = lipgloss.NewStyle().Foreground(o.fgMostSubtle)
	s.Dialog.Quit.Frame = lipgloss.NewStyle().BorderForeground(o.primary).Border(lipgloss.RoundedBorder()).Padding(1, 2)
	s.Dialog.View = base.Border(lipgloss.RoundedBorder()).BorderForeground(o.primary)
	s.Dialog.PrimaryText = base.Padding(0, 1).Foreground(o.primary)
	s.Dialog.SecondaryText = base.Padding(0, 1).Foreground(o.fgMostSubtle)
	s.Dialog.HelpView = base.Padding(0, 1).AlignHorizontal(lipgloss.Left)
	s.Dialog.Help.ShortKey = base.Foreground(o.fgMoreSubtle)
	s.Dialog.Help.ShortDesc = base.Foreground(o.fgMostSubtle)
	s.Dialog.Help.ShortSeparator = base.Foreground(o.separator)
	s.Dialog.Help.Ellipsis = base.Foreground(o.separator)
	s.Dialog.Help.FullKey = base.Foreground(o.fgMoreSubtle)
	s.Dialog.Help.FullDesc = base.Foreground(o.fgMostSubtle)
	s.Dialog.Help.FullSeparator = base.Foreground(o.separator)
	s.Dialog.NormalItem = base.Padding(0, 1).Foreground(o.fgBase)
	s.Dialog.SelectedItem = base.Padding(0, 1).Background(o.primary).Foreground(o.onPrimary)
	s.Dialog.InputPrompt = base.Margin(1, 1)

	s.Dialog.List = base.Margin(0, 0, 1, 0)
	s.Dialog.ContentPanel = base.Background(o.bgLessVisible).Foreground(o.fgBase).Padding(1, 2)
	s.Dialog.ContentPanelBg = o.bgLessVisible
	s.Dialog.Spinner = base.Foreground(o.secondary)
	s.Dialog.ScrollbarThumb = base.Foreground(o.secondary)
	s.Dialog.ScrollbarTrack = base.Foreground(o.separator)

	s.Dialog.ImagePreview = lipgloss.NewStyle().Padding(0, 1).Foreground(o.fgMostSubtle)

	// API key input dialog
	s.Dialog.APIKey.Spinner = base.Foreground(o.success)

	// Auth method choice dialog: the selected card lights up in the success
	// color while the other stays quiet.
	s.Dialog.AuthMethod.Prompt = base.Padding(0, 1)
	s.Dialog.AuthMethod.CardBlurred = base.Border(lipgloss.RoundedBorder()).BorderForeground(o.separator).Foreground(o.fgMostSubtle)
	s.Dialog.AuthMethod.CardFocused = base.Border(lipgloss.RoundedBorder()).BorderForeground(o.success).Foreground(o.success)

	// OAuth dialog
	s.Dialog.OAuth.Spinner = base.Foreground(o.successMoreSubtle)
	s.Dialog.OAuth.Instructions = lipgloss.NewStyle().Foreground(o.fgBase)
	s.Dialog.OAuth.UserCode = lipgloss.NewStyle().Bold(true).Foreground(o.fgBase)
	s.Dialog.OAuth.Success = lipgloss.NewStyle().Foreground(o.successMoreSubtle)
	s.Dialog.OAuth.Link = lipgloss.NewStyle().Foreground(o.successMostSubtle).Underline(true)
	s.Dialog.OAuth.Enter = lipgloss.NewStyle().Foreground(o.keyword)
	s.Dialog.OAuth.ErrorText = lipgloss.NewStyle().Foreground(o.error)
	s.Dialog.OAuth.StatusText = lipgloss.NewStyle().Foreground(o.fgMoreSubtle)
	s.Dialog.OAuth.UserCodeBg = o.bgLeastVisible

	s.Dialog.Arguments.Content = base.Padding(1)
	s.Dialog.Arguments.Description = base.MarginBottom(1).MaxHeight(3)
	s.Dialog.Arguments.InputLabelBlurred = base.Foreground(o.fgMoreSubtle)
	s.Dialog.Arguments.InputLabelFocused = base.Bold(true)
	s.Dialog.Arguments.InputRequiredMarkBlurred = base.Foreground(o.fgMoreSubtle).SetString("*")
	s.Dialog.Arguments.InputRequiredMarkFocused = base.Foreground(o.primary).Bold(true).SetString("*")

	s.Dialog.Sessions.DeletingTitle = s.Dialog.Title.Foreground(o.destructive)
	s.Dialog.Sessions.DeletingView = s.Dialog.View.BorderForeground(o.destructive)
	s.Dialog.Sessions.DeletingMessage = base.Padding(1)
	s.Dialog.Sessions.DeletingTitleGradientFromColor = o.destructive
	s.Dialog.Sessions.DeletingTitleGradientToColor = o.primary
	s.Dialog.Sessions.DeletingItemBlurred = s.Dialog.NormalItem.Foreground(o.fgMostSubtle)
	s.Dialog.Sessions.DeletingItemFocused = s.Dialog.SelectedItem.Background(o.destructive).Foreground(o.onPrimary)

	s.Dialog.Sessions.RenamingingTitle = s.Dialog.Title.Foreground(o.warningSubtle)
	s.Dialog.Sessions.RenamingView = s.Dialog.View.BorderForeground(o.warningSubtle)
	s.Dialog.Sessions.RenamingingMessage = base.Padding(1)
	s.Dialog.Sessions.RenamingTitleGradientFromColor = o.warningSubtle
	s.Dialog.Sessions.RenamingTitleGradientToColor = o.accent
	s.Dialog.Sessions.RenamingItemBlurred = s.Dialog.NormalItem.Foreground(o.fgMostSubtle)
	s.Dialog.Sessions.RenamingingItemFocused = s.Dialog.SelectedItem.UnsetBackground().UnsetForeground()
	s.Dialog.Sessions.RenamingPlaceholder = base.Foreground(o.fgMoreSubtle)
	s.Dialog.Sessions.InfoBlurred = lipgloss.NewStyle().Foreground(o.fgMostSubtle)
	s.Dialog.Sessions.InfoFocused = lipgloss.NewStyle().Foreground(o.fgBase)

	s.Status.Help = lipgloss.NewStyle().Padding(0, 1)
	s.Status.ModeBadgePlan = lipgloss.NewStyle().Foreground(o.plan).Padding(0, 1).Bold(true).SetString("PLAN MODE")
	s.Status.ModeBadgeYolo = lipgloss.NewStyle().Foreground(o.busy).Padding(0, 1).Bold(true).SetString("YOLO MODE")
	s.Status.ModeBadgeRemote = lipgloss.NewStyle().Foreground(o.info).Padding(0, 1).Bold(true).SetString("REMOTE")
	s.Status.ModeBannerPlanBadge = s.Status.ModeBadgePlan
	s.Status.ModeBannerPlan = lipgloss.NewStyle().Foreground(o.fgBase).Background(o.planMoreSubtle).Padding(0, 1)
	s.Status.ModeBannerYoloBadge = s.Status.ModeBadgeYolo
	s.Status.ModeBannerYolo = lipgloss.NewStyle().Foreground(o.bgBase).Background(o.yolo).Padding(0, 1)
	s.Status.SuccessIndicator = base.Foreground(o.success).Background(o.bgLeastVisible).Padding(0, 1).Bold(true).SetString("OKAY!")
	s.Status.InfoIndicator = s.Status.SuccessIndicator.Foreground(o.info)
	s.Status.UpdateIndicator = s.Status.SuccessIndicator.Foreground(o.accent).SetString("HEY!")
	s.Status.WarnIndicator = s.Status.SuccessIndicator.Foreground(o.warning).SetString("WARNING")
	s.Status.ErrorIndicator = s.Status.SuccessIndicator.Foreground(o.error).SetString("ERROR")
	s.Status.SuccessMessage = base.Foreground(o.fgSubtle).Background(o.bgLeastVisible).Padding(0, 1)
	s.Status.InfoMessage = s.Status.SuccessMessage
	s.Status.UpdateMessage = s.Status.SuccessMessage
	s.Status.WarnMessage = s.Status.SuccessMessage
	s.Status.ErrorMessage = s.Status.SuccessMessage
}

// quickStyleCompletions sets the completions, attachments and pills styles.
func quickStyleCompletions(s *Styles, o quickStyleOpts, base lipgloss.Style) {
	// Completions styles
	s.Completions.Normal = base.Background(o.bgLessVisible).Foreground(o.fgBase)
	s.Completions.Focused = base.Background(o.primary).Foreground(o.onPrimary)
	s.Completions.Match = base.Underline(true)

	// Attachments styles
	attachmentIconStyle := base.Foreground(o.bgLessVisible).Background(o.success).Padding(0, 1)
	s.Attachments.Image = attachmentIconStyle.SetString(ImageIcon)
	s.Attachments.Text = attachmentIconStyle.SetString(TextIcon)
	s.Attachments.Skill = attachmentIconStyle.SetString(SkillIcon)
	s.Attachments.Normal = base.Padding(0, 1).Background(o.fgMoreSubtle).Foreground(o.fgBase)
	s.Attachments.KeptIcon = base.Foreground(o.bgBase).Background(o.plan).Padding(0, 1).SetString("on")
	s.Attachments.KeptName = base.Padding(0, 1).Background(o.planMoreSubtle).Foreground(o.fgBase)
	s.Attachments.KeptDot = base.Foreground(o.plan).SetString("●")
	s.Attachments.KeptText = base.Foreground(o.plan)
	// Remove and Deleting share the same slot on the right side of a chip
	// and must keep the same geometry so toggling delete-mode doesn't
	// shift the chips. Padding(0, 1) puts a colored cell on each side of the
	// glyph so it isn't flush against the box edge, while MarginRight(1)
	// keeps a transparent gap between adjacent chips.
	s.Attachments.Remove = base.Padding(0, 1).MarginRight(1).Background(o.bgLessVisible).Foreground(o.fgSubtle).SetString(RemoveIcon)
	s.Attachments.Deleting = base.Padding(0, 1).MarginRight(1).Bold(true).Background(o.destructive).Foreground(o.fgBase)
	s.Attachments.More = base.Foreground(o.fgSubtle)

	// Pills styles
	s.Pills.Base = base.Padding(0, 1)
	s.Pills.Focused = base.Padding(0, 1).BorderStyle(lipgloss.RoundedBorder()).BorderForeground(o.bgMostVisible)
	s.Pills.QueueItemPrefix = lipgloss.NewStyle().Foreground(o.fgMoreSubtle).SetString("  •")
	s.Pills.QueueItemText = lipgloss.NewStyle().Foreground(o.fgMoreSubtle)
	s.Pills.QueueLabel = lipgloss.NewStyle().Foreground(o.fgBase)
	s.Pills.QueueIconBase = lipgloss.NewStyle().Foreground(o.fgBase)
	s.Pills.QueueGradFromColor = o.error
	s.Pills.QueueGradToColor = o.secondary
	s.Pills.TodoLabel = lipgloss.NewStyle().Foreground(o.fgBase)
	s.Pills.TodoProgress = lipgloss.NewStyle().Foreground(o.fgMoreSubtle)
	s.Pills.TodoCurrentTask = lipgloss.NewStyle().Foreground(o.fgMostSubtle)
	s.Pills.TodoSpinner = lipgloss.NewStyle().Foreground(o.successMostSubtle)
	s.Pills.HelpKey = lipgloss.NewStyle().Foreground(o.fgMoreSubtle)
	s.Pills.HelpText = lipgloss.NewStyle().Foreground(o.fgMostSubtle)
	s.Pills.Area = base
}

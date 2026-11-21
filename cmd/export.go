package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/iksnae/cursor-session/internal"
	"github.com/iksnae/cursor-session/internal/export"
	"github.com/spf13/cobra"
)

var nonAlphanumericRegex = regexp.MustCompile(`[^a-z0-9]+`)

// slugifyTitle converts a session title like "How do I make the API?" into "how-do-i-make-the-api"
func slugifyTitle(title string) string {
	slug := strings.ToLower(strings.TrimSpace(title))
	slug = nonAlphanumericRegex.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 100 {
		slug = strings.Trim(slug[:100], "-")
	}
	return slug
}

// formatSessionTimestampPrefix extracts the session timestamp (CreatedAt, UpdatedAt, or first message timestamp)
// and formats it as "YYYY-MM-DD:HH-MM-SS". Returns empty string if no valid timestamp is found.
func formatSessionTimestampPrefix(session *internal.Session) string {
	candidates := []string{
		session.Metadata.CreatedAt,
		session.Metadata.UpdatedAt,
	}
	for _, msg := range session.Messages {
		if msg.Timestamp != "" {
			candidates = append(candidates, msg.Timestamp)
			break
		}
	}

	for _, raw := range candidates {
		if raw == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			return t.Format("2006-01-02:15-04-05")
		}
		if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			return t.Format("2006-01-02:15-04-05")
		}
	}

	return ""
}

// buildSessionFilename returns the export filename for a session, prefixed with its timestamp
// (e.g. "2026-10-20:10-30-00_how-do-i-add-the-tests.md"), falling back to session_<id> if untitled,
// and deduplicating collisions within the same export run.
func buildSessionFilename(session *internal.Session, ext string, usedNames map[string]int) string {
	baseName := slugifyTitle(session.Metadata.Name)
	if baseName == "" {
		baseName = fmt.Sprintf("session_%s", session.ID)
	}

	if tsPrefix := formatSessionTimestampPrefix(session); tsPrefix != "" {
		baseName = fmt.Sprintf("%s_%s", tsPrefix, baseName)
	}

	if usedNames != nil {
		usedNames[baseName]++
		if count := usedNames[baseName]; count > 1 {
			baseName = fmt.Sprintf("%s-%d", baseName, count)
		}
	}

	return fmt.Sprintf("%s.%s", baseName, ext)
}

var (
	format       string
	outputDir    string
	workspace    string
	sessionID    string
	intermediary bool
	clearCache   bool
)

// exportCmd represents the export command
var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export sessions to file",
	Long: `Export chat sessions to various formats (jsonl, md, yaml, json).

You can export all sessions, filter by workspace, or export a specific session by ID.
Use 'cursor-session list' to see available session IDs.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get paths (with optional custom storage location)
		paths, err := internal.GetStoragePaths(storagePath)
		if err != nil {
			return fmt.Errorf("failed to get storage paths: %w", err)
		}

		// Copy database files to temp location if --copy flag is set
		var cleanup func() error
		if copyDB {
			var copyErr error
			paths, cleanup, copyErr = internal.CopyStoragePaths(paths)
			if copyErr != nil {
				return fmt.Errorf("failed to copy database files: %w", copyErr)
			}
			// Schedule cleanup when command completes
			defer func() {
				if cleanup != nil {
					if err := cleanup(); err != nil {
						internal.LogWarn("Failed to cleanup temporary files: %v", err)
					} else {
						internal.LogInfo("Cleaned up temporary database files")
					}
				}
			}()
		}

		// Create storage backend (handles both desktop app and agent storage)
		backend, err := internal.NewStorageBackend(paths)
		if err != nil {
			return fmt.Errorf("failed to initialize storage: %w", err)
		}

		// Initialize cache manager (always enabled)
		// Store cache in user's home directory root
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		cacheDir := filepath.Join(homeDir, ".cursor-session-cache")
		cacheManager := internal.NewCacheManager(cacheDir)

		// Clear cache if requested
		if clearCache {
			if err := cacheManager.ClearCache(); err != nil {
				internal.LogWarn("Failed to clear cache: %v", err)
			} else {
				internal.LogInfo("Cache cleared")
			}
		}

		var sessions []*internal.Session

		// Use appropriate cache key based on storage type
		var cacheKey string
		if paths.GlobalStorageExists() {
			cacheKey = paths.GetGlobalStorageDBPath()
		} else if paths.HasAgentStorage() {
			cacheKey = paths.AgentStoragePath
		} else {
			cacheKey = "unknown"
		}

		// Fast-path when exporting a single session by ID
		if sessionID != "" {
			var targetSession *internal.Session

			// Check cache index first (unless cache was just cleared)
			if index, err := cacheManager.LoadIndex(); err == nil && index != nil && index.Metadata.DatabasePath == cacheKey {
				for _, entry := range index.Sessions {
					if matchSessionID(entry.ComposerID, sessionID) || matchSessionID(entry.ID, sessionID) {
						if loaded, err := cacheManager.LoadSession(entry.ID); err == nil {
							targetSession = loaded
							internal.LogInfo("Loaded session %s from cache", entry.ID)
							break
						}
					}
				}
			}

			// Reconstruct only the requested session from storage if not in cache
			if targetSession == nil {
				targetComposer, bubbles, contexts, err := internal.LoadSingleSessionData(backend, sessionID)
				if err != nil {
					return err
				}
				if targetComposer == nil {
					return fmt.Errorf("session not found: %s (use 'cursor-session list' to see available sessions)", sessionID)
				}

				bubbleMap := internal.NewBubbleMap()
				for _, bubble := range bubbles {
					bubbleMap.Set(bubble.BubbleID, bubble)
				}

				reconstructor := internal.NewReconstructor(bubbleMap, contexts)
				conv, err := reconstructor.ReconstructConversation(targetComposer)
				if err != nil {
					return fmt.Errorf("failed to reconstruct conversation: %w", err)
				}

				assignedWorkspace := workspace
				if assignedWorkspace == "" {
					workspaces, _ := internal.DetectWorkspaces(paths.BasePath)
					assignedWorkspace = internal.AssociateComposerWithWorkspace(conv.ComposerID, contexts[conv.ComposerID], workspaces)
				}

				normalizer := internal.NewNormalizer()
				targetSession, err = normalizer.NormalizeConversation(conv, assignedWorkspace)
				if err != nil {
					return fmt.Errorf("failed to normalize conversation: %w", err)
				}

				_ = cacheManager.SaveSessionAndUpdateIndex(targetSession, cacheKey)
			}

			sessions = []*internal.Session{targetSession}
		}

		// Try to load all sessions from cache when exporting all sessions
		if sessions == nil {
			valid, err := cacheManager.IsCacheValid(cacheKey)
			if err == nil && valid {
				internal.LogInfo("Loading sessions from cache...")
				sessions, err = cacheManager.LoadAllSessions()
				if err == nil && len(sessions) > 0 {
					internal.LogInfo("Loaded %d session(s) from cache", len(sessions))
				} else {
					internal.LogWarn("Failed to load cache: %v, reconstructing...", err)
					sessions = nil
				}
			}
		}

		// Reconstruct if cache miss
		if sessions == nil {
			var conversations []*internal.ReconstructedConversation
			var loadedContexts map[string][]*internal.MessageContext

			ctx := context.Background()
			steps := []internal.ProgressStep{
				{
					Message: "Loading data from storage",
					Fn: func() error {
						var loadErr error
						bubbleChan, composerChan, contextChan, loadErr := internal.LoadDataAsyncFromBackend(backend)
						if loadErr != nil {
							return fmt.Errorf("failed to load data: %w", loadErr)
						}

						// Intercept contexts so we don't query the database a second time during normalization
						forwardedContextChan := make(chan *internal.MessageContext, 100)
						loadedContexts = make(map[string][]*internal.MessageContext)
						go func() {
							defer close(forwardedContextChan)
							for msgCtx := range contextChan {
								if msgCtx != nil {
									loadedContexts[msgCtx.ComposerID] = append(loadedContexts[msgCtx.ComposerID], msgCtx)
								}
								forwardedContextChan <- msgCtx
							}
						}()

						// Reconstruct conversations
						conversations, loadErr = internal.ReconstructAsync(bubbleChan, composerChan, forwardedContextChan)
						if loadErr != nil {
							return fmt.Errorf("failed to reconstruct conversations: %w", loadErr)
						}
						return nil
					},
				},
				{
					Message: "Processing and normalizing sessions",
					Fn: func() error {
						// Detect workspaces for association
						workspaces, _ := internal.DetectWorkspaces(paths.BasePath)

						// Normalize with workspace association using already-loaded contexts
						normalizer := internal.NewNormalizer()
						sessions = make([]*internal.Session, 0, len(conversations))
						for _, conv := range conversations {
							// Try to associate with workspace
							assignedWorkspace := workspace
							if assignedWorkspace == "" {
								assignedWorkspace = internal.AssociateComposerWithWorkspace(conv.ComposerID, loadedContexts[conv.ComposerID], workspaces)
							}

							session, err := normalizer.NormalizeConversation(conv, assignedWorkspace)
							if err != nil {
								internal.LogWarn("Failed to normalize conversation %s: %v", conv.ComposerID, err)
								continue
							}
							sessions = append(sessions, session)
						}

						// Log summary statistics
						internal.LogInfo("Normalization complete: %d composers processed, %d sessions created", len(conversations), len(sessions))

						// Deduplicate
						deduplicator := internal.NewDeduplicator()
						sessions = deduplicator.Deduplicate(sessions)
						return nil
					},
				},
				{
					Message: "Caching sessions",
					Fn: func() error {
						// Save to cache
						if err := cacheManager.SaveSessions(sessions, cacheKey); err != nil {
							internal.LogWarn("Failed to save cache: %v", err)
						}
						return nil
					},
				},
			}

			if err := internal.ShowProgressWithSteps(ctx, steps); err != nil {
				return err
			}
		}

		// Filter by workspace if specified
		if workspace != "" {
			filtered := make([]*internal.Session, 0)
			for _, session := range sessions {
				if session.Workspace == workspace {
					filtered = append(filtered, session)
				}
			}
			sessions = filtered
		}

		// Filter by session ID if specified (supports full ID or prefix shorthand)
		if sessionID != "" {
			filtered := make([]*internal.Session, 0)
			for _, session := range sessions {
				if matchSessionID(session.ID, sessionID) || matchSessionID(session.Metadata.ComposerID, sessionID) {
					filtered = append(filtered, session)
					break // Only one session should match
				}
			}
			if len(filtered) == 0 {
				return fmt.Errorf("session not found: %s (use 'cursor-session list' to see available sessions)", sessionID)
			}
			sessions = filtered
		}

		// Create exporter
		exporter, err := export.NewExporter(format)
		if err != nil {
			return err
		}

		// Ensure output directory exists
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return fmt.Errorf("failed to create output directory: %w", err)
		}

		// Export sessions with progress
		ctx := context.Background()
		err = internal.ShowProgress(ctx, fmt.Sprintf("Exporting %d session(s) to %s", len(sessions), outputDir), func() error {
			usedNames := make(map[string]int)
			for _, session := range sessions {
				if session == nil {
					internal.LogWarn("Skipping nil session")
					continue
				}
				filename := buildSessionFilename(session, exporter.Extension(), usedNames)
				filepath := filepath.Join(outputDir, filename)

				file, err := os.Create(filepath)
				if err != nil {
					internal.LogError("Failed to create file %s: %v", filepath, err)
					continue
				}

				if err := exporter.Export(session, file); err != nil {
					_ = file.Close()
					internal.LogError("Failed to export session %s: %v", session.ID, err)
					continue
				}

				if err := file.Close(); err != nil {
					internal.LogWarn("Failed to close file %s: %v", filepath, err)
				}
			}
			return nil
		})
		if err != nil {
			return err
		}

		internal.PrintSuccess(fmt.Sprintf("Export complete: %d session(s) exported to %s", len(sessions), outputDir))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(exportCmd)
	exportCmd.Flags().StringVarP(&format, "format", "f", "jsonl", "Export format (jsonl, md, yaml, json)")
	exportCmd.Flags().StringVarP(&outputDir, "out", "o", "./exports", "Output directory")
	exportCmd.Flags().StringVar(&workspace, "workspace", "", "Filter by workspace")
	exportCmd.Flags().StringVar(&sessionID, "session-id", "", "Export a specific session by ID")
	exportCmd.Flags().BoolVar(&intermediary, "intermediary", false, "Save intermediary format")
	exportCmd.Flags().BoolVar(&clearCache, "clear-cache", false, "Clear the cache before running")
}

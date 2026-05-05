# Development Roadmap & Known Bugs

## Short-term Goals (v0.2.0)
- Ensure all butler agent features are functional, add special tools and feature examples
  - API fill-in tool (ask interaction tool upgrade)
  - A knowledge base aggregating all encountered issues
  - Allow users to manually clear loaded files to prevent AI from forgetting to clean up, and automatically trigger cleanup when files reach a specified count

## Medium-term Goals (v0.3.0)
- Subagent system
- Long-term memory functionality
- Search tool return results may need to add IDs (paragraph - two-character hash)
- After deleting files, need to update AI's loaded file list
- Subagent, multi-agent system
- Git checkpoint feature with branch support
- Automated testing for some features
- Multimodal, image upload
- Visual workflow editor (similar to Dify)
- Left/right paging for message checkpoint restoration
- When releasing interruption, user messages and tool results should optimistically update in order (tool result first, then user message), may require backend refactoring to merge different checkpoint data before sending to frontend for rendering, which would also enable message paging
- ComfyUI-related features
- More flexible AI chat features (similar to Tavern?)
- Tool enhancements
- Notify provider to stop generation during interruption? Need to reference other projects for implementation
- Without API key, AI can forcibly create knowledge bases using backend ports, and can't even delete them?
- Allow users to open folders as working directories
- Add `memory_edit` tool, allowing AI to directly edit its own context (? no clear ideas yet)

## Long-term Goals (v1.0.0)
- To be determined

## Known Bugs

### Tool permission disabled after use causes error
When AI previously used a tool (e.g., `write_file`), and the tool's permissions and usage instructions are subsequently disabled, if AI attempts to call this disabled tool, it will error. But it's controllable — after rendering crash, open the message and delete the last call.

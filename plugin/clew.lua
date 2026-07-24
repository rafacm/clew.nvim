-- Guard so the plugin is only loaded once, and only on a supported Neovim.
if vim.g.loaded_clew then return end
vim.g.loaded_clew = true

if vim.fn.has("nvim-0.11") ~= 1 then
  vim.notify("clew.nvim requires Neovim 0.11+", vim.log.levels.WARN, { title = "clew" })
  return
end

-- Commands are registered here rather than in setup() so that :ClewStatus and
-- :checkhealth clew work even when the user has not called setup() yet -- which
-- is exactly the situation where they need the diagnostics.
require("clew.commands").setup()

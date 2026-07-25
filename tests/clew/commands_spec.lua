local commands = require("clew.commands")

describe("clew.commands", function()
  -- Index age is the user-facing half of staleness_check: the index reports
  -- positions as of the last index, so `gd` drifts after edits and the age is
  -- how a user knows to distrust it.
  describe("_humanise", function()
    it("reports seconds under a minute", function()
      assert.equals("0s ago", commands._humanise(0))
      assert.equals("59s ago", commands._humanise(59))
    end)

    it("reports minutes under an hour", function()
      assert.equals("1m ago", commands._humanise(60))
      assert.equals("59m ago", commands._humanise(3599))
    end)

    it("reports hours under a day", function()
      assert.equals("1h ago", commands._humanise(3600))
      assert.equals("23h ago", commands._humanise(86399))
    end)

    it("reports days beyond that", function()
      assert.equals("1d ago", commands._humanise(86400))
      assert.equals("30d ago", commands._humanise(86400 * 30))
    end)
  end)

  describe("setup", function()
    -- doc/clew.txt documents exactly these three. A previous drift documented
    -- two commands that did not exist.
    it("registers the documented user commands and no others", function()
      commands.setup()
      local registered = vim.api.nvim_get_commands({})
      for _, name in ipairs({ "ClewIndex", "ClewStatus", "ClewRestart" }) do
        assert.is_truthy(registered[name], name .. " was not registered")
      end
      for name in pairs(registered) do
        if name:match("^Clew") then
          assert.is_true(
            name == "ClewIndex" or name == "ClewStatus" or name == "ClewRestart",
            "undocumented command registered: " .. name
          )
        end
      end
    end)

    it("gives ClewIndex an optional unit argument", function()
      commands.setup()
      assert.equals("?", vim.api.nvim_get_commands({})["ClewIndex"].nargs)
    end)
  end)
end)

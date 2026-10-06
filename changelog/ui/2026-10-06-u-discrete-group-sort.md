# Grouping and sorting are picked by name

The board's settings dialog now has a "group by" picker with named rules: project (the default), repo, room, status,
runner, tag prefix, pile, tag, your groups, age, and your own code. An "order the groups by" picker offers name, most
recent activity, card count, and your own code. The two code boxes show only when "your own code" is picked. A named
choice is saved in this browser with the rest of the grouping, and no expression is stored for it. Code written before
the pickers existed keeps running.

The repos tab has a sort control: name, last push, or date added. It is remembered across reloads. The hub now reports
when it took each repo in (`created`) so "date added" has something to sort by.

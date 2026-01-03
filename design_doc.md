# Dunbar 
Dunbar is a PRM designed to help me keep in touch with people I care about

# What do I suck at 

1. Planning and setting travel 
2. Keeping in touch with people I care about 

# What would be ideal

1. Knowing in advance about trips and coordinating with those people 
2. Staying touch with people (ideally in adventures that feel natural) 
3. Having a system that reminds me to reach out to people regularly
4. Having a way to document my adventures (maybe even share them?)
5. Integration for planning for adventure tasks added to my todo system 


# Architecture

App is local only, but will fetch data from external sources 

1. Contact Manager 
    - Local database of contacts (name, phone, email, last contacted date, notes)
    - Syncs with google contacts via an interface
    - allows adding custom tags to each contact

1. Message Manager
    - Interface to messaging platform (via API) to track what messages have been connected 

1. Message Frequency System 
    - Simple system when run looks for a contact that has not been contacted by a threshold time (set in the contact info
    - Generates a reminder task in my todo system (via ATP) to contact that person
    - Allows us to set a contact frequency to any contact



# MVP
1. We need basic contact management
2. We need a way to detect time between communication (some sort of basic text integration) 
3. We need a reminder to contact added to my list / text 


# Interface
- Golang
- Primarily local all files stored in a configurable folder passed by the DUNBAR_DIR env var 
- Each system is resonsible for its own data storage 
- Use cli based interface based on bonzai
